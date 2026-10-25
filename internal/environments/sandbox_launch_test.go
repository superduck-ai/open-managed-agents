package environments

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestApplySessionSandboxLaunchKeepsPlatformEnv(t *testing.T) {
	resolution := applySessionSandboxLaunch(e2bruntime.Resolution{
		Timeout: 30 * time.Second,
		Envs:    map[string]string{"ANTHROPIC_ENVIRONMENT_ID": "env_test"},
	}, []byte(`{"timeout_seconds":60,"env":{"TRACE":"1","ANTHROPIC_ENVIRONMENT_ID":"other"}}`))
	if resolution.Timeout != time.Minute || resolution.Envs["TRACE"] != "1" || resolution.Envs["ANTHROPIC_ENVIRONMENT_ID"] != "env_test" {
		t.Fatalf("resolution = %#v", resolution)
	}
	unchanged := applySessionSandboxLaunch(e2bruntime.Resolution{Timeout: 30 * time.Second}, []byte(`{}`))
	if unchanged.Timeout != 30*time.Second || unchanged.Envs != nil {
		t.Fatalf("unchanged = %#v", unchanged)
	}
}

func TestManagedAgentSessionConfigIncludesEnvironmentVariables(t *testing.T) {
	raw, err := managedAgentSessionConfig(db.Session{
		Metadata: json.RawMessage(`{"timeout_seconds":"60","env":"{\"TRACE\":\"1\"}"}`),
	}, mustResolveRuntimeResources(t, nil), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"TRACE":"1"`) {
		t.Fatalf("config = %s", raw)
	}
	withoutEnv, err := managedAgentSessionConfig(db.Session{}, mustResolveRuntimeResources(t, nil), nil, "")
	if err != nil || strings.Contains(string(withoutEnv), "environment_variables") {
		t.Fatalf("config = %s err=%v", withoutEnv, err)
	}
}
