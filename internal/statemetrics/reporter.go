package statemetrics

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
	PoolStat() *pgxpool.Stat
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
	stream      string
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

func (r *Reporter) Start(ctx context.Context) (func(), error) {
	registration, err := r.register(otel.Meter(telemetry.InstrumentationName))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go r.loop(ctx, done)
	return func() {
		cancel()
		<-done
		if err := registration.Unregister(); err != nil {
			r.logger.WarnContext(context.Background(), "unregister state metrics", "error", err)
		}
	}, nil
}

func (r *Reporter) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	r.refresh(ctx)
	ticker := time.NewTicker(r.deps.Config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refresh(ctx)
		}
	}
}

func (r *Reporter) refresh(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, r.deps.Config.Interval/2)
	defer cancel()
	now := time.Now().UTC()
	states, err := r.deps.DB.OperationalStateCounts(queryCtx, now.Add(-r.deps.Config.StaleRunningAfter), now.Add(-r.deps.Config.StuckStartingAfter))
	if err != nil {
		r.logger.WarnContext(ctx, "collect operational state counts", "error", err)
		r.store(snapshot{})
		return
	}
	r.store(snapshot{
		ready:     true,
		states:    states,
		streams:   r.collectStreams(queryCtx, now),
		consumers: r.collectConsumers(queryCtx),
	})
}

func (r *Reporter) store(next snapshot) {
	r.mu.Lock()
	r.snapshot = next
	r.mu.Unlock()
}

func (r *Reporter) current() snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

func (r *Reporter) collectStreams(ctx context.Context, now time.Time) []streamState {
	states := make([]streamState, 0, len(r.deps.Streams))
	for _, name := range r.deps.Streams {
		stream, err := r.js.Stream(ctx, name)
		if err != nil {
			r.logger.WarnContext(ctx, "collect NATS stream", "stream", name, "error", err)
			continue
		}
		info, err := stream.Info(ctx)
		if err != nil {
			r.logger.WarnContext(ctx, "collect NATS stream info", "stream", name, "error", err)
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

func (r *Reporter) collectConsumers(ctx context.Context) []consumerState {
	states := make([]consumerState, 0, len(r.deps.Consumers))
	for _, target := range r.deps.Consumers {
		consumer, err := r.js.Consumer(ctx, target.Stream, target.Consumer)
		if err != nil {
			r.logger.WarnContext(ctx, "collect NATS consumer", "stream", target.Stream, "consumer", target.Consumer, "error", err)
			continue
		}
		info, err := consumer.Info(ctx)
		if err != nil {
			r.logger.WarnContext(ctx, "collect NATS consumer info", "stream", target.Stream, "consumer", target.Consumer, "error", err)
			continue
		}
		states = append(states, consumerState{stream: target.Stream, name: target.Consumer, pending: int64(info.NumPending), ackPending: int64(info.NumAckPending), redelivered: int64(info.NumRedelivered)})
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
	brainClaimsPending             metric.Int64ObservableGauge
	brainClaimsOldestWaitingAge    metric.Int64ObservableGauge
	natsStreamMessages             metric.Int64ObservableGauge
	natsStreamOldestMessageAge     metric.Int64ObservableGauge
	natsConsumerPending            metric.Int64ObservableGauge
	natsConsumerAckPending         metric.Int64ObservableGauge
	natsConsumerRedelivered        metric.Int64ObservableGauge
	dbPoolConnections              metric.Int64ObservableGauge
}

func (r *Reporter) register(meter metric.Meter) (metric.Registration, error) {
	definitions := []struct {
		target                  *metric.Int64ObservableGauge
		name, unit, description string
	}{
		{&r.gauges.sessionsLive, "oma.sessions.live", "{session}", "Live sessions"},
		{&r.gauges.sessionsStaleRunning, "oma.sessions.stale_running", "{session}", "Stale running sessions"},
		{&r.gauges.codeSessionsActive, "oma.code_sessions.active", "{code_session}", "Active code sessions"},
		{&r.gauges.environmentWorkLive, "oma.environment_work.live", "{work}", "Live environment work"},
		{&r.gauges.environmentWorkOldestQueuedAge, "oma.environment_work.oldest_queued_age", "s", "Oldest queued environment work age"},
		{&r.gauges.environmentWorkStuckStarting, "oma.environment_work.stuck_starting", "{work}", "Stuck starting environment work"},
		{&r.gauges.environmentSandboxesLive, "oma.environment_sandboxes.live", "{sandbox}", "Live environment sandboxes"},
		{&r.gauges.brainClaimsPending, "oma.brain_claims.pending", "{code_session}", "Code sessions waiting for a Brain worker"},
		{&r.gauges.brainClaimsOldestWaitingAge, "oma.brain_claims.oldest_waiting_age", "s", "Oldest unclaimed Brain worker request age"},
		{&r.gauges.natsStreamMessages, "oma.nats.stream.messages", "{message}", "NATS stream messages"},
		{&r.gauges.natsStreamOldestMessageAge, "oma.nats.stream.oldest_message_age", "s", "NATS stream oldest message age"},
		{&r.gauges.natsConsumerPending, "oma.nats.consumer.pending", "{message}", "NATS consumer pending messages"},
		{&r.gauges.natsConsumerAckPending, "oma.nats.consumer.ack_pending", "{message}", "NATS consumer messages pending acknowledgment"},
		{&r.gauges.natsConsumerRedelivered, "oma.nats.consumer.redelivered", "{message}", "NATS consumer redelivered messages"},
		{&r.gauges.dbPoolConnections, "oma.db.pool.connections", "{connection}", "Database pool connections"},
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
	return meter.RegisterCallback(r.observe, observables...)
}

func (r *Reporter) observe(_ context.Context, observer metric.Observer) error {
	stat := r.deps.DB.PoolStat()
	for state, value := range map[string]int64{"acquired": int64(stat.AcquiredConns()), "idle": int64(stat.IdleConns()), "total": int64(stat.TotalConns()), "max": int64(stat.MaxConns())} {
		observer.ObserveInt64(r.gauges.dbPoolConnections, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	current := r.current()
	if !current.ready {
		return nil
	}
	for state, value := range current.states.Sessions {
		observer.ObserveInt64(r.gauges.sessionsLive, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	observer.ObserveInt64(r.gauges.sessionsStaleRunning, current.states.StaleRunningSessions)
	for state, value := range current.states.CodeSessions {
		observer.ObserveInt64(r.gauges.codeSessionsActive, value, metric.WithAttributes(telemetry.AttrWorkerStatus.String(state.WorkerStatus), telemetry.AttrConnectionStatus.String(state.ConnectionStatus)))
	}
	for state, value := range current.states.EnvironmentWork {
		observer.ObserveInt64(r.gauges.environmentWorkLive, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	observer.ObserveInt64(r.gauges.environmentWorkOldestQueuedAge, current.states.OldestQueuedWorkSeconds)
	observer.ObserveInt64(r.gauges.environmentWorkStuckStarting, current.states.StuckStartingWork)
	for state, value := range current.states.EnvironmentSandboxes {
		observer.ObserveInt64(r.gauges.environmentSandboxesLive, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	for state, value := range current.states.BrainClaims {
		observer.ObserveInt64(r.gauges.brainClaimsPending, value, metric.WithAttributes(telemetry.AttrState.String(state)))
	}
	observer.ObserveInt64(r.gauges.brainClaimsOldestWaitingAge, current.states.OldestWaitingBrainClaimSeconds)
	for _, state := range current.streams {
		attrs := metric.WithAttributes(telemetry.AttrNATSStream.String(state.name))
		observer.ObserveInt64(r.gauges.natsStreamMessages, state.messages, attrs)
		observer.ObserveInt64(r.gauges.natsStreamOldestMessageAge, state.oldestAge, attrs)
	}
	for _, state := range current.consumers {
		attrs := metric.WithAttributes(telemetry.AttrNATSStream.String(state.stream), telemetry.AttrNATSConsumer.String(state.name))
		observer.ObserveInt64(r.gauges.natsConsumerPending, state.pending, attrs)
		observer.ObserveInt64(r.gauges.natsConsumerAckPending, state.ackPending, attrs)
		observer.ObserveInt64(r.gauges.natsConsumerRedelivered, state.redelivered, attrs)
	}
	return nil
}
