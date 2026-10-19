package statemetrics

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

type testStateDatabase struct {
	pool   *pgxpool.Pool
	counts db.OperationalStateCounts
	err    error
	calls  atomic.Int64
}

func (d *testStateDatabase) PoolStat() *pgxpool.Stat { return d.pool.Stat() }
func (d *testStateDatabase) OperationalStateCounts(ctx context.Context, _, _ time.Time) (db.OperationalStateCounts, error) {
	d.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return db.OperationalStateCounts{}, err
	}
	return d.counts, d.err
}

func reporterForTest(t *testing.T) (*Reporter, *testStateDatabase) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://metrics:metrics@127.0.0.1:1/metrics?pool_min_conns=0&pool_max_conns=4")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	database := &testStateDatabase{pool: pool}
	return &Reporter{deps: Dependencies{DB: database, Config: config.TelemetryStateMetricsConfig{Interval: time.Minute, StaleRunningAfter: time.Hour, StuckStartingAfter: time.Minute}}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, database
}

func TestReporterObservesOnlyPoolUntilReady(t *testing.T) {
	reporter, database := reporterForTest(t)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	registration, err := reporter.register(provider.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registration.Unregister() })
	collect := func() map[string]metricdata.Gauge[int64] {
		var result metricdata.ResourceMetrics
		if err := reader.Collect(t.Context(), &result); err != nil {
			t.Fatal(err)
		}
		data := map[string]metricdata.Gauge[int64]{}
		for _, scope := range result.ScopeMetrics {
			for _, m := range scope.Metrics {
				if gauge, ok := m.Data.(metricdata.Gauge[int64]); ok {
					data[m.Name] = gauge
				}
			}
		}
		return data
	}
	data := collect()
	if len(data) != 1 || len(data["oma.db.pool.connections"].DataPoints) != 4 || database.calls.Load() != 0 {
		t.Fatalf("unready observations: %v", data)
	}
	for _, point := range data["oma.db.pool.connections"].DataPoints {
		state, _ := point.Attributes.Value(attribute.Key("oma.state"))
		want := int64(0)
		if state.AsString() == "max" {
			want = 4
		}
		if point.Value != want || point.Attributes.Len() != 1 {
			t.Fatalf("pool point: %+v", point)
		}
	}
	codeSessions := map[db.CodeSessionState]int64{}
	for _, worker := range []string{"unknown", "idle", "running", "requires_action"} {
		for _, connection := range []string{"connected", "disconnected"} {
			codeSessions[db.CodeSessionState{WorkerStatus: worker, ConnectionStatus: connection}] = 0
		}
	}
	codeSessions[db.CodeSessionState{WorkerStatus: "running", ConnectionStatus: "connected"}] = 3
	reporter.store(snapshot{ready: true, states: db.OperationalStateCounts{
		Sessions: map[string]int64{"running": 2, "rescheduling": 0}, StaleRunningSessions: 1, CodeSessions: codeSessions,
		EnvironmentWork: map[string]int64{"queued": 4, "starting": 0, "active": 0, "stopping": 0}, OldestQueuedWorkSeconds: 5, StuckStartingWork: 6,
		EnvironmentSandboxes: map[string]int64{"creating": 7, "running": 0, "stopping": 0},
		BrainClaims:          map[string]int64{"waiting": 13, "claimed": 0, "takeover": 0}, OldestWaitingBrainClaimSeconds: 14,
	}, streams: []streamState{{name: "OMA_WORKER_INBOUND", messages: 8, oldestAge: 9}}, consumers: []consumerState{{name: "oma_webhook_delivery", pending: 10, ackPending: 11, redelivered: 12}}})
	data = collect()
	expected := map[string]int{"oma.sessions.live": 2, "oma.sessions.stale_running": 1, "oma.code_sessions.active": 8, "oma.environment_work.live": 4, "oma.environment_work.oldest_queued_age": 1, "oma.environment_work.stuck_starting": 1, "oma.environment_sandboxes.live": 3, "oma.brain_claims.pending": 3, "oma.brain_claims.oldest_waiting_age": 1, "oma.nats.stream.messages": 1, "oma.nats.stream.oldest_message_age": 1, "oma.nats.consumer.pending": 1, "oma.nats.consumer.ack_pending": 1, "oma.nats.consumer.redelivered": 1, "oma.db.pool.connections": 4}
	if len(data) != len(expected) {
		t.Fatalf("gauge names: %v", data)
	}
	for name, count := range expected {
		if len(data[name].DataPoints) != count {
			t.Fatalf("%s points=%d want %d", name, len(data[name].DataPoints), count)
		}
	}
	for name, want := range map[string]int64{"oma.sessions.live": 2, "oma.sessions.stale_running": 1, "oma.code_sessions.active": 3, "oma.environment_work.live": 4, "oma.environment_work.oldest_queued_age": 5, "oma.environment_work.stuck_starting": 6, "oma.environment_sandboxes.live": 7, "oma.brain_claims.pending": 13, "oma.brain_claims.oldest_waiting_age": 14, "oma.nats.stream.messages": 8, "oma.nats.stream.oldest_message_age": 9, "oma.nats.consumer.pending": 10, "oma.nats.consumer.ack_pending": 11, "oma.nats.consumer.redelivered": 12} {
		var total int64
		for _, point := range data[name].DataPoints {
			total += point.Value
			for _, attr := range point.Attributes.ToSlice() {
				switch attr.Key {
				case "oma.state", "oma.worker.status", "oma.connection.status", "oma.nats.stream", "oma.nats.consumer":
				default:
					t.Fatalf("unexpected gauge attribute: %v", attr)
				}
			}
		}
		if total != want {
			t.Fatalf("%s=%d want %d", name, total, want)
		}
	}
	if database.calls.Load() != 0 {
		t.Fatal("metric observation performed database query")
	}
	database.err = errors.New("database unavailable")
	reporter.refresh(t.Context())
	data = collect()
	if reporter.current().ready || len(data) != 1 {
		t.Fatalf("failed snapshot remained visible: %v", data)
	}
	database.err = nil
	database.counts.StaleRunningSessions = 2
	reporter.refresh(t.Context())
	if !reporter.current().ready || reporter.current().states.StaleRunningSessions != 2 {
		t.Fatal("successful snapshot not restored")
	}
}

func TestReporterStartStopUnregisters(t *testing.T) {
	reporter, database := reporterForTest(t)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(previous); _ = provider.Shutdown(context.Background()) })
	stop, err := reporter.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for database.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stop()
	if database.calls.Load() == 0 {
		t.Fatal("initial refresh did not run")
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if _, ok := m.Data.(metricdata.Gauge[int64]); ok {
				t.Fatalf("callback remained registered: %s", m.Name)
			}
		}
	}
}

func TestCollectFailureLabels(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	for _, reason := range []string{"database", "nats"} {
		recordCollectFailure(t.Context(), reason)
	}
	points := telemetrytest.Points(t, telemetrytest.Collect(t, reader), "oma.state_metrics.collect.failures")
	if len(points) != 2 {
		t.Fatalf("failure points: %v", points)
	}
	for _, point := range points {
		reason, _ := point.Attributes.Value("oma.reason")
		telemetrytest.AssertAttributes(t, "oma.state_metrics.collect.failures", point.Attributes, map[string]string{"oma.reason": reason.AsString()})
		if point.Value != 1 || (reason.AsString() != "database" && reason.AsString() != "nats") {
			t.Fatalf("failure point: %+v", point)
		}
	}
}
