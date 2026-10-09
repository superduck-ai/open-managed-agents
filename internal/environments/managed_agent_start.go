package environments

import (
	"context"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (r *Runner) startPreparedAgent(ctx context.Context, env db.Environment, record db.EnvironmentSandbox, work *db.EnvironmentWork, sandboxID string, preparation managedAgentLaunchPreparation) error {
	launch, err := r.createManagedAgentRuntimeLaunch(ctx, env, *work, preparation)
	if err != nil {
		r.failCreatedSandbox(ctx, record, work, sandboxID, err)
		return fmt.Errorf("create managed-agent runtime launch: %w", err)
	}
	if err := r.provider.StartBackgroundCommand(ctx, sandboxID, launch.Manager.ShellCommand, launch.Manager.Payload); err != nil {
		publicError := r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_start", errEnvironmentManagerStart, err)
		r.failManagedAgentRuntime(ctx, record, work, sandboxID, preparation.Session, launch, publicError)
		return publicError
	}
	if err := r.publishManagedAgentRuntime(ctx, preparation.Session, *work, launch); err != nil {
		r.failManagedAgentRuntime(ctx, record, work, sandboxID, preparation.Session, launch, err)
		return fmt.Errorf("publish managed-agent runtime metadata: %w", err)
	}
	if launch.Mode == agentruntime.Host {
		if err := r.startHostAgent(ctx, sandboxID, launch); err != nil {
			r.failManagedAgentRuntime(ctx, record, work, sandboxID, preparation.Session, launch, err)
			return err
		}
	}
	return nil
}
