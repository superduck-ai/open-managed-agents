package environments

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func (r *Runner) resolveEnvironmentSandbox(env db.Environment, work *db.EnvironmentWork) (e2bruntime.Resolution, templateSource, error) {
	variables, err := environmentVariablesFromConfig(env.Config)
	if err != nil {
		return e2bruntime.Resolution{}, "", err
	}
	resolution, err := r.provider.Resolve(env, work)
	if err != nil {
		return e2bruntime.Resolution{}, "", err
	}
	resolution.Envs = sandboxEnvironmentVariables(variables, resolution.Envs)
	return resolution, environmentTemplateSource(env), nil
}

func (r *Runner) reportTemplateSource(ctx context.Context, run *runnerRun, env db.Environment, work *db.EnvironmentWork, template string, source templateSource) {
	run.markTemplateSource(source)
	if source != templateSourceDefault {
		return
	}
	spec, err := decodeEnvironmentBuildSpec(env.Config)
	if err != nil || !spec.requiresTemplate() {
		return
	}
	r.logger.WarnContext(ctx, "environment template spec not applied",
		"environment_id", env.ExternalID, "workspace_id", env.WorkspaceUUID,
		"work_id", work.ExternalID, "session_id", work.SessionExternalID,
		"template", template, "template_source", string(source))
}
