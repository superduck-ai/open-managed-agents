package environments

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sandboxEndpointResolver interface {
	ServiceEndpoint(context.Context, string, int, string) (string, error)
}

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

func (r *Runner) sandboxMCPEndpoint(ctx context.Context, sandboxID string) (string, error) {
	resolver, ok := r.provider.(sandboxEndpointResolver)
	if !ok {
		return "", errSandboxEndpointUnsupported
	}
	port, path := r.cfg.EnvironmentRunner.SandboxMCPPort, r.cfg.EnvironmentRunner.SandboxMCPPath
	if port == 0 {
		port = 3001
	}
	if path == "" {
		path = "/mcp"
	}
	return resolver.ServiceEndpoint(ctx, sandboxID, port, path)
}
