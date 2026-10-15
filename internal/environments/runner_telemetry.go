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
	stage      string
	stageEnd   func(error)
	stageStart time.Time
}

func startRunnerRun(ctx context.Context, work db.EnvironmentWork) (context.Context, *runnerRun) {
	ctx, span := telemetry.StartRoot(ctx, "oma.runner.run", work.TraceParent,
		telemetry.AttrWorkID.String(work.ExternalID),
		telemetry.AttrSessionID.String(work.SessionExternalID),
		telemetry.AttrWorkspaceUUID.String(work.WorkspaceUUID),
		telemetry.AttrEnvironmentID.String(work.EnvironmentExternalID),
	)
	now := time.Now()
	if work.QueuedAt != nil {
		queueWait := now.Sub(*work.QueuedAt)
		span.SetAttributes(attribute.Float64("oma.runner.queue_wait_s", queueWait.Seconds()))
		telemetry.RecordRunnerQueueWait(ctx, queueWait)
	}
	run := &runnerRun{ctx: ctx, span: span, sessionID: work.SessionExternalID, startedAt: now}
	return run.begin("claim"), run
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
	telemetry.RecordRunnerRun(r.ctx, telemetry.Outcome(err), r.recovered, time.Since(r.startedAt))
	telemetry.End(r.span, err)
}

func (r *runnerRun) closeStage(err error) {
	if r.stageEnd == nil {
		return
	}
	telemetry.RecordRunnerStage(r.ctx, r.stage, telemetry.Outcome(err), time.Since(r.stageStart))
	r.stageEnd(err)
	r.stageEnd = nil
}

func isRecoveryLaunch(preparation *managedAgentLaunchPreparation) bool {
	return preparation != nil && preparation.RecoveryCodeSessionID != ""
}
