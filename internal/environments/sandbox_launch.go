package environments

import (
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func applySessionSandboxLaunch(resolution e2bruntime.Resolution, metadata json.RawMessage) e2bruntime.Resolution {
	if timeout := sessioncontract.SandboxTimeout(metadata); timeout > 0 {
		resolution.Timeout = timeout
	}
	env := sessioncontract.SandboxEnv(metadata)
	if len(env) == 0 {
		return resolution
	}
	if resolution.Envs == nil {
		resolution.Envs = map[string]string{}
	}
	for key, value := range env {
		if _, exists := resolution.Envs[key]; exists {
			continue
		}
		resolution.Envs[key] = value
	}
	return resolution
}
