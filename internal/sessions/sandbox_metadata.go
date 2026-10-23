package sessions

import (
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func environmentWorkMetadata(sessionMetadata json.RawMessage) json.RawMessage {
	timeout := sessioncontract.SandboxTimeout(sessionMetadata)
	env := sessioncontract.SandboxEnv(sessionMetadata)
	if timeout <= 0 && len(env) == 0 {
		return json.RawMessage(`{}`)
	}
	body := map[string]any{}
	if timeout > 0 {
		body[sessioncontract.SandboxTimeoutMetadataKey] = int(timeout / time.Second)
	}
	if len(env) > 0 {
		body[sessioncontract.SandboxEnvMetadataKey] = env
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}
