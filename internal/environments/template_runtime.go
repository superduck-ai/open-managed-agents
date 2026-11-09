package environments

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func (r *Runner) resolveEnvironmentSandbox(env db.Environment, work *db.EnvironmentWork) (e2bruntime.Resolution, bool, error) {
	preinstalled := hasPrebuiltTemplate(env)
	spec, err := decodeEnvironmentBuildSpec(env.Config)
	if err != nil {
		return e2bruntime.Resolution{}, false, err
	}
	if spec.requiresTemplate() && !preinstalled {
		return e2bruntime.Resolution{}, false, errEnvironmentTemplateNotReady
	}
	variables, err := environmentVariablesFromConfig(env.Config)
	if err != nil {
		return e2bruntime.Resolution{}, false, err
	}
	resolution, err := r.provider.Resolve(env, work)
	if err != nil {
		return e2bruntime.Resolution{}, false, err
	}
	resolution.Envs = sandboxEnvironmentVariables(variables, resolution.Envs)
	return resolution, preinstalled, nil
}
