package environments

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sandboxEndpointResolver interface {
	ServiceEndpoint(context.Context, string, int, string) (string, error)
}

func (r *Runner) finishManagedAgentRuntime(ctx context.Context, run *runnerRun, session db.Session, work db.EnvironmentWork, launch managedAgentRuntimeLaunch) error {
	ctx = run.begin("publish_runtime")
	if err := r.publishManagedAgentRuntime(ctx, session, work, launch); err != nil {
		return err
	}
	if r.cfg.EnvironmentRunner.AgentMode != "brain" {
		return nil
	}
	dispatcher, ok := r.codeSessions.(interface {
		DispatchWorker(context.Context, codesessions.ManagedAgentCreateResult) error
	})
	if !ok {
		return errWorkerDispatchUnavailable
	}
	ctx = run.begin("brain_dispatch")
	return dispatcher.DispatchWorker(ctx, launch.Credentials)
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
