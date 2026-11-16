package environments

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (r *Runner) finishManagedAgentRuntime(ctx context.Context, run *runnerRun, session db.Session, work db.EnvironmentWork, launch managedAgentRuntimeLaunch) error {
	ctx = run.begin(ctx, "publish_runtime")
	if err := r.publishManagedAgentRuntime(ctx, session, work, launch); err != nil {
		return err
	}
	if r.cfg.EnvironmentRunner.AgentMode != "brain" {
		return nil
	}
	dispatcher, ok := r.codeSessions.(interface {
		DispatchWorker(context.Context, db.Session, string) error
	})
	if !ok {
		return errWorkerDispatchUnavailable
	}
	ctx = run.begin(ctx, "brain_dispatch")
	return dispatcher.DispatchWorker(ctx, session, launch.CodeSessionID)
}
