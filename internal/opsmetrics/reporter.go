package opsmetrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type ConsumerTarget struct {
	Stream   string
	Consumer string
}

type stateDatabase interface {
	OperationalStateCounts(context.Context, time.Time, time.Time) (db.OperationalStateCounts, error)
	ListenerPool() *pgxpool.Pool
}

type Dependencies struct {
	DB        stateDatabase
	NATS      *nats.Conn
	Config    config.TelemetryStateMetricsConfig
	Streams   []string
	Consumers []ConsumerTarget
	Logger    *slog.Logger
}

type Reporter struct {
	deps     Dependencies
	js       jetstream.JetStream
	logger   *slog.Logger
	mu       sync.RWMutex
	snapshot snapshot
	gauges   gauges
}

type snapshot struct {
	ready     bool
	states    db.OperationalStateCounts
	streams   []streamState
	consumers []consumerState
}

type streamState struct {
	name      string
	messages  int64
	oldestAge int64
}

type consumerState struct {
	name        string
	pending     int64
	ackPending  int64
	redelivered int64
}

func New(deps Dependencies) (*Reporter, error) {
	js, err := jetstream.New(deps.NATS)
	if err != nil {
		return nil, err
	}
	return &Reporter{deps: deps, js: js, logger: logging.LoggerOrDefault(deps.Logger)}, nil
}

func (c *Reporter) Start(ctx context.Context) (func(), error) {
	registration, err := c.register(otel.Meter(telemetry.InstrumentationName))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go c.loop(ctx, done)
	return func() {
		cancel()
		<-done
		if err := registration.Unregister(); err != nil {
			c.logger.WarnContext(context.Background(), "unregister state metrics", "error", err)
		}
	}, nil
}

func (c *Reporter) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	c.refresh(ctx)
	ticker := time.NewTicker(c.deps.Config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

func (c *Reporter) refresh(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, c.deps.Config.Interval/2)
	defer cancel()
	now := time.Now().UTC()
	states, err := c.deps.DB.OperationalStateCounts(queryCtx, now.Add(-c.deps.Config.StaleRunningAfter), now.Add(-c.deps.Config.StuckStartingAfter))
	if err != nil {
		c.logger.WarnContext(ctx, "collect operational state counts", "error", err)
		telemetry.RecordStateMetricsFailure(ctx, "database")
		c.store(snapshot{})
		return
	}
	c.store(snapshot{
		ready:     true,
		states:    states,
		streams:   c.collectStreams(queryCtx, now),
		consumers: c.collectConsumers(queryCtx),
	})
}

func (c *Reporter) store(next snapshot) {
	c.mu.Lock()
	c.snapshot = next
	c.mu.Unlock()
}

func (c *Reporter) current() snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

func (c *Reporter) collectStreams(ctx context.Context, now time.Time) []streamState {
	states := make([]streamState, 0, len(c.deps.Streams))
	for _, name := range c.deps.Streams {
		stream, err := c.js.Stream(ctx, name)
		if err != nil {
			c.logger.WarnContext(ctx, "collect NATS stream", "stream", name, "error", err)
			telemetry.RecordStateMetricsFailure(ctx, "nats")
			continue
		}
		info, err := stream.Info(ctx)
		if err != nil {
			c.logger.WarnContext(ctx, "collect NATS stream info", "stream", name, "error", err)
			telemetry.RecordStateMetricsFailure(ctx, "nats")
			continue
		}
		state := streamState{name: name, messages: int64(info.State.Msgs)}
		if info.State.Msgs > 0 && !info.State.FirstTime.IsZero() {
			state.oldestAge = max(0, int64(now.Sub(info.State.FirstTime).Seconds()))
		}
		states = append(states, state)
	}
	return states
}

func (c *Reporter) collectConsumers(ctx context.Context) []consumerState {
	states := make([]consumerState, 0, len(c.deps.Consumers))
	for _, target := range c.deps.Consumers {
		consumer, err := c.js.Consumer(ctx, target.Stream, target.Consumer)
		if err != nil {
			c.logger.WarnContext(ctx, "collect NATS consumer", "stream", target.Stream, "consumer", target.Consumer, "error", err)
			telemetry.RecordStateMetricsFailure(ctx, "nats")
			continue
		}
		info, err := consumer.Info(ctx)
		if err != nil {
			c.logger.WarnContext(ctx, "collect NATS consumer info", "stream", target.Stream, "consumer", target.Consumer, "error", err)
			telemetry.RecordStateMetricsFailure(ctx, "nats")
			continue
		}
		states = append(states, consumerState{name: target.Consumer, pending: int64(info.NumPending), ackPending: int64(info.NumAckPending), redelivered: int64(info.NumRedelivered)})
	}
	return states
}

type gauges struct {
	sessionsLive                   metric.Int64ObservableGauge
	sessionsStaleRunning           metric.Int64ObservableGauge
	codeSessionsActive             metric.Int64ObservableGauge
	environmentWorkLive            metric.Int64ObservableGauge
	environmentWorkOldestQueuedAge metric.Int64ObservableGauge
	environmentWorkStuckStarting   metric.Int64ObservableGauge
	environmentSandboxesLive       metric.Int64ObservableGauge
	natsStreamMessages             metric.Int64ObservableGauge
	natsStreamOldestMessageAge     metric.Int64ObservableGauge
	natsConsumerPending            metric.Int64ObservableGauge
	natsConsumerAckPending         metric.Int64ObservableGauge
	natsConsumerRedelivered        metric.Int64ObservableGauge
	dbPoolConnections              metric.Int64ObservableGauge
}

func (c *Reporter) register(meter metric.Meter) (metric.Registration, error) {
	definitions := []struct {
		target                  *metric.Int64ObservableGauge
		name, unit, description string
	}{
		{&c.gauges.sessionsLive, "oma.sessions.live", "{session}", "Live sessions"},
		{&c.gauges.sessionsStaleRunning, "oma.sessions.stale_running", "{session}", "Stale running sessions"},
		{&c.gauges.codeSessionsActive, "oma.code_sessions.active", "{code_session}", "Active code sessions"},
		{&c.gauges.environmentWorkLive, "oma.environment_work.live", "{work}", "Live environment work"},
		{&c.gauges.environmentWorkOldestQueuedAge, "oma.environment_work.oldest_queued_age", "s", "Oldest queued environment work age"},
		{&c.gauges.environmentWorkStuckStarting, "oma.environment_work.stuck_starting", "{work}", "Stuck starting environment work"},
		{&c.gauges.environmentSandboxesLive, "oma.environment_sandboxes.live", "{sandbox}", "Live environment sandboxes"},
		{&c.gauges.natsStreamMessages, "oma.nats.stream.messages", "{message}", "NATS stream messages"},
		{&c.gauges.natsStreamOldestMessageAge, "oma.nats.stream.oldest_message_age", "s", "NATS stream oldest message age"},
		{&c.gauges.natsConsumerPending, "oma.nats.consumer.pending", "{message}", "NATS consumer pending messages"},
		{&c.gauges.natsConsumerAckPending, "oma.nats.consumer.ack_pending", "{message}", "NATS consumer messages pending acknowledgment"},
		{&c.gauges.natsConsumerRedelivered, "oma.nats.consumer.redelivered", "{message}", "NATS consumer redelivered messages"},
		{&c.gauges.dbPoolConnections, "oma.db.pool.connections", "{connection}", "Database pool connections"},
	}
	observables := make([]metric.Observable, len(definitions))
	for i, definition := range definitions {
		gauge, err := meter.Int64ObservableGauge(definition.name, metric.WithUnit(definition.unit), metric.WithDescription(definition.description))
		if err != nil {
			return nil, err
		}
		*definition.target = gauge
		observables[i] = gauge
	}
	return meter.RegisterCallback(c.observe, observables...)
}

func (c *Reporter) observe(_ context.Context, observer metric.Observer) error {
	stat := c.deps.DB.ListenerPool().Stat()
	for state, value := range map[string]int64{"acquired": int64(stat.AcquiredConns()), "idle": int64(stat.IdleConns()), "total": int64(stat.TotalConns()), "max": int64(stat.MaxConns())} {
		observer.ObserveInt64(c.gauges.dbPoolConnections, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	current := c.current()
	if !current.ready {
		return nil
	}
	for state, value := range withZeroStates(current.states.Sessions, []string{"running", "rescheduling"}) {
		observer.ObserveInt64(c.gauges.sessionsLive, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	observer.ObserveInt64(c.gauges.sessionsStaleRunning, current.states.StaleRunningSessions)
	for state, value := range withZeroCodeSessionStates(current.states.CodeSessions) {
		observer.ObserveInt64(c.gauges.codeSessionsActive, value, metric.WithAttributes(telemetry.AttrWorkerStatus.String(state.worker), telemetry.AttrConnectionStatus.String(state.connection)))
	}
	for state, value := range withZeroStates(current.states.EnvironmentWork, []string{"queued", "starting", "active", "stopping"}) {
		observer.ObserveInt64(c.gauges.environmentWorkLive, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	observer.ObserveInt64(c.gauges.environmentWorkOldestQueuedAge, current.states.OldestQueuedWorkSeconds)
	observer.ObserveInt64(c.gauges.environmentWorkStuckStarting, current.states.StuckStartingWork)
	for state, value := range withZeroStates(current.states.EnvironmentSandboxes, []string{"creating", "running", "stopping"}) {
		observer.ObserveInt64(c.gauges.environmentSandboxesLive, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	for _, state := range current.streams {
		attrs := metric.WithAttributes(telemetry.AttrNATSStream.String(state.name))
		observer.ObserveInt64(c.gauges.natsStreamMessages, state.messages, attrs)
		observer.ObserveInt64(c.gauges.natsStreamOldestMessageAge, state.oldestAge, attrs)
	}
	for _, state := range current.consumers {
		attrs := metric.WithAttributes(telemetry.AttrNATSConsumer.String(state.name))
		observer.ObserveInt64(c.gauges.natsConsumerPending, state.pending, attrs)
		observer.ObserveInt64(c.gauges.natsConsumerAckPending, state.ackPending, attrs)
		observer.ObserveInt64(c.gauges.natsConsumerRedelivered, state.redelivered, attrs)
	}
	return nil
}

func withZeroStates(counts []db.StateCount, states []string) map[string]int64 {
	totals := make(map[string]int64, len(states))
	for _, state := range states {
		totals[state] = 0
	}
	for _, count := range counts {
		totals[count.State] = count.Total
	}
	return totals
}

type codeSessionState struct {
	worker     string
	connection string
}

func withZeroCodeSessionStates(counts []db.CodeSessionStateCount) map[codeSessionState]int64 {
	totals := make(map[codeSessionState]int64, 8)
	for _, worker := range []string{"unknown", "idle", "running", "requires_action"} {
		for _, connection := range []string{"connected", "disconnected"} {
			totals[codeSessionState{worker: worker, connection: connection}] = 0
		}
	}
	for _, count := range counts {
		totals[codeSessionState{worker: count.WorkerStatus, connection: count.ConnectionStatus}] = count.Total
	}
	return totals
}
