package environments

import (
	"context"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

const managerVersionProbeLimit = 10 * time.Second

func parseEnvironmentManagerVersion(output []byte) (string, bool) {
	if len(output) > 1024 {
		return "", false
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 || (fields[0] != "environment-manager" && fields[0] != "environment-runner") {
		return "", false
	}
	version := fields[1]
	return version, semver.IsValid("v" + version)
}

func (r *Runner) reuseEnvironmentManager(ctx context.Context, sandboxID, managerPath string) (bool, error) {
	required := r.managerArtifacts.cfg.Version
	if required == "" {
		return false, nil
	}
	result, err := r.provider.RunCommand(ctx, sandboxID, e2bruntime.CommandRequest{
		Command: shellQuote(managerPath) + " --version",
		Timeout: managerVersionProbeLimit,
	})
	if ctx.Err() != nil {
		return false, r.logManagedAgentRuntimeStageFailure(ctx, "environment_manager_version_probe", errEnvironmentManagerInstall, ctx.Err())
	}
	if err != nil || result.ExitCode != 0 {
		return false, nil
	}
	version, valid := parseEnvironmentManagerVersion(result.Stdout)
	if !valid || semver.Compare("v"+version, "v"+required) < 0 {
		return false, nil
	}
	r.logger.InfoContext(ctx, "environment manager artifact reused", "sandbox_id", sandboxID, "manager_path", managerPath, "manager_version", version, "required_manager_version", required)
	return true, nil
}
