package environments

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

const (
	runnerTriggerQueue         = "queue"
	runnerTriggerStaleRecovery = "stale_recovery"
	runnerTriggerLazyMCP       = "lazy_mcp"
	runnerTriggerPrewarm       = "prewarm"
)

type runnerInstruments struct {
	queueWait     metric.Float64Histogram
	runs          metric.Int64Counter
	runDuration   metric.Float64Histogram
	stageDuration metric.Float64Histogram
	sandboxEnsure metric.Float64Histogram
}

var runnerMetrics = telemetry.NewInstruments(func(meter metric.Meter) runnerInstruments {
	return runnerInstruments{
		queueWait:     telemetry.Histogram(meter, "oma.runner.queue_wait", "s", "Runner queue wait", telemetry.LongWaitBuckets()),
		runs:          telemetry.Counter(meter, "oma.runner.runs", "{run}", "Runner runs"),
		runDuration:   telemetry.Histogram(meter, "oma.runner.run.duration", "s", "Runner run duration", telemetry.LatencyBuckets()),
		stageDuration: telemetry.Histogram(meter, "oma.runner.stage.duration", "s", "Runner stage duration", telemetry.LatencyBuckets()),
		sandboxEnsure: telemetry.Histogram(meter, "oma.sandbox.ensure.duration", "s", "Session sandbox ensure duration", telemetry.LatencyBuckets()),
	}
})

type runnerRun struct {
	span           trace.Span
	sessionID      string
	startedAt      time.Time
	recovered      bool
	trigger        string
	endRun         func(error)
	stage          string
	endStage       func(error)
	stageStartedAt time.Time
}

func startRunnerRun(ctx context.Context, work db.EnvironmentWork) (context.Context, *runnerRun) {
	trigger := runnerTriggerQueue
	if work.State != "queued" {
		trigger = runnerTriggerStaleRecovery
	}
	ctx, span := telemetry.StartRoot(ctx, "oma.runner.run", work.TraceParent, runnerRunAttributes(work, trigger)...)
	now := time.Now()
	if trigger == runnerTriggerStaleRecovery {
		span.SetAttributes(staleRecoveryAttributes(work, now)...)
	} else if work.QueuedAt != nil {
		queueWait := now.Sub(*work.QueuedAt)
		span.SetAttributes(attribute.Float64("oma.runner.queue_wait_s", queueWait.Seconds()))
		telemetry.RecordWait(ctx, runnerMetrics.Get().queueWait, queueWait)
	}
	run := &runnerRun{span: span, sessionID: work.SessionExternalID, startedAt: now, trigger: trigger, endRun: func(err error) {
		telemetry.SetErrorStatus(span, err)
		span.End()
	}}
	return run.begin(ctx, "claim"), run
}

func startSandboxRun(ctx context.Context, work db.EnvironmentWork, trigger string) (context.Context, *runnerRun) {
	ctx, span := telemetry.Start(ctx, "oma.runner.run", runnerRunAttributes(work, trigger)...)
	run := &runnerRun{span: span, sessionID: work.SessionExternalID, startedAt: time.Now(), trigger: trigger, endRun: func(err error) {
		telemetry.SetErrorStatus(span, err)
		span.End()
	}}
	return run.begin(ctx, "prepare"), run
}

func staleRecoveryAttributes(work db.EnvironmentWork, now time.Time) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attribute.String("oma.environment_work.state", work.State)}
	if work.LatestHeartbeatAt != nil {
		attrs = append(attrs, attribute.Float64("oma.environment_work.heartbeat_age_s", now.Sub(*work.LatestHeartbeatAt).Seconds()))
	}
	return attrs
}

func runnerRunAttributes(work db.EnvironmentWork, trigger string) []attribute.KeyValue {
	return []attribute.KeyValue{
		telemetry.AttrWorkID.String(work.ExternalID),
		telemetry.AttrSessionID.String(work.SessionExternalID),
		telemetry.AttrWorkspaceUUID.String(work.WorkspaceUUID),
		telemetry.AttrEnvironmentID.String(work.EnvironmentExternalID),
		telemetry.AttrRunnerTrigger.String(trigger),
	}
}

func (r *runnerRun) begin(ctx context.Context, stage string) context.Context {
	r.closeStage(ctx, nil)
	ctx, span := telemetry.Start(trace.ContextWithSpan(ctx, r.span), "oma.runner."+stage,
		telemetry.AttrStage.String(stage),
		telemetry.AttrSessionID.String(r.sessionID),
	)
	r.stage, r.stageStartedAt = stage, time.Now()
	r.endStage = func(err error) {
		telemetry.SetErrorStatus(span, err)
		span.End()
	}
	return ctx
}

func (r *runnerRun) markRecovered(recovered bool) {
	if r.trigger == runnerTriggerLazyMCP || r.trigger == runnerTriggerPrewarm {
		recovered = false
	}
	r.recovered = recovered
	r.span.SetAttributes(telemetry.AttrRecovered.Bool(recovered))
}

func (r *runnerRun) end(ctx context.Context, err error) {
	failedStage := r.stage
	r.closeStage(ctx, err)
	if err != nil {
		r.span.SetAttributes(telemetry.AttrStage.String(failedStage))
	}
	attrs := metric.WithAttributes(telemetry.AttrRunnerTrigger.String(r.trigger), telemetry.AttrOutcome.String(telemetry.Outcome(err)), telemetry.AttrRecovered.Bool(r.recovered))
	instruments := runnerMetrics.Get()
	instruments.runs.Add(ctx, 1, attrs)
	instruments.runDuration.Record(ctx, time.Since(r.startedAt).Seconds(), attrs)
	r.endRun(err)
}

func (r *runnerRun) closeStage(ctx context.Context, err error) {
	if r.endStage == nil {
		return
	}
	runnerMetrics.Get().stageDuration.Record(ctx, time.Since(r.stageStartedAt).Seconds(), metric.WithAttributes(
		telemetry.AttrRunnerTrigger.String(r.trigger), telemetry.AttrStage.String(r.stage), telemetry.AttrOutcome.String(telemetry.Outcome(err))))
	r.endStage(err)
	r.endStage = nil
}

func recordSandboxEnsure(ctx context.Context, trigger, outcome string, startedAt time.Time) {
	runnerMetrics.Get().sandboxEnsure.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(telemetry.AttrRunnerTrigger.String(trigger), telemetry.AttrOutcome.String(outcome)))
}

func isRecoveryLaunch(preparation *managedAgentLaunchPreparation) bool {
	return preparation != nil && preparation.RecoveryCodeSessionID != ""
}
