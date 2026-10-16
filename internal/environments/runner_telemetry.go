package environments

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

type runnerRun struct {
	ctx        context.Context
	span       trace.Span
	sessionID  string
	startedAt  time.Time
	recovered  bool
	trigger    string
	spanEnd    func(error)
	stage      string
	stageEnd   func(error)
	stageStart time.Time
}

func startRunnerRun(ctx context.Context, work db.EnvironmentWork) (context.Context, *runnerRun) {
	trigger := telemetry.RunnerTriggerQueue
	if work.State != "queued" {
		trigger = telemetry.RunnerTriggerStaleRecovery
	}
	ctx, span := telemetry.StartRoot(ctx, "oma.runner.run", work.TraceParent, runnerRunAttributes(work, trigger)...)
	now := time.Now()
	if trigger == telemetry.RunnerTriggerStaleRecovery {
		telemetry.RecordRunnerStaleRecoveryClaim(ctx)
	} else if work.QueuedAt != nil {
		queueWait := now.Sub(*work.QueuedAt)
		span.SetAttributes(attribute.Float64("oma.runner.queue_wait_s", queueWait.Seconds()))
		telemetry.RecordRunnerQueueWait(ctx, queueWait)
	}
	run := newRunnerRun(ctx, span, work, trigger, now)
	run.spanEnd = func(err error) {
		defer span.End()
		telemetry.SetErrorStatus(span, err)
	}
	return run.begin("claim"), run
}

func startSandboxRun(ctx context.Context, work db.EnvironmentWork, trigger string) (context.Context, *runnerRun) {
	ctx, span := telemetry.Start(ctx, "oma.runner.run", runnerRunAttributes(work, trigger)...)
	run := newRunnerRun(ctx, span, work, trigger, time.Now())
	run.spanEnd = func(err error) {
		defer span.End()
		telemetry.SetErrorStatus(span, err)
	}
	return run.begin("prepare"), run
}

func newRunnerRun(ctx context.Context, span trace.Span, work db.EnvironmentWork, trigger string, startedAt time.Time) *runnerRun {
	return &runnerRun{ctx: ctx, span: span, sessionID: work.SessionExternalID, startedAt: startedAt, trigger: trigger}
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

func (r *runnerRun) begin(stage string) context.Context {
	r.closeStage(nil)
	ctx, span := telemetry.Start(r.ctx, "oma.runner."+stage,
		telemetry.AttrStage.String(stage),
		telemetry.AttrSessionID.String(r.sessionID),
	)
	r.stage = stage
	r.stageEnd = func(err error) {
		defer span.End()
		telemetry.SetErrorStatus(span, err)
	}
	r.stageStart = time.Now()
	return ctx
}

func (r *runnerRun) markRecovered(recovered bool) {
	r.recovered = recovered
	r.span.SetAttributes(telemetry.AttrRecovered.Bool(recovered))
}

func (r *runnerRun) end(err error) {
	failedStage := r.stage
	r.closeStage(err)
	if err != nil {
		r.span.SetAttributes(telemetry.AttrStage.String(failedStage))
	}
	telemetry.RecordRunnerRun(r.ctx, r.trigger, telemetry.Outcome(err), r.recovered, time.Since(r.startedAt))
	r.spanEnd(err)
}

func (r *runnerRun) closeStage(err error) {
	if r.stageEnd == nil {
		return
	}
	telemetry.RecordRunnerStage(r.ctx, r.trigger, r.stage, telemetry.Outcome(err), time.Since(r.stageStart))
	r.stageEnd(err)
	r.stageEnd = nil
}

func isRecoveryLaunch(preparation *managedAgentLaunchPreparation) bool {
	return preparation != nil && preparation.RecoveryCodeSessionID != ""
}
