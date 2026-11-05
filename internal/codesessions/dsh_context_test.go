package codesessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDSHChildContextRejectsInvalidSnapshots(t *testing.T) {
	for _, raw := range []string{`{"subagents":{"agents":[{"name":"child"}]}}`, `{"subagents":{"agents":[{"name":"child","agentSnapshot":[]}]}}`} {
		snapshot := dshContextSnapshot{Config: json.RawMessage(raw)}
		record := db.CodeSession{Metadata: json.RawMessage(`{"config":{}}`)}
		if _, err := dshChildContexts(record, snapshot, MCPRuntimeIdentity{}, config.TunnelConfig{}); err == nil {
			t.Fatalf("accepted=%s", raw)
		}
	}
}

func TestDSHChildContextsUsePresetPermissionsAndCurrentCredentials(t *testing.T) {
	snapshot := dshContextSnapshot{Config: json.RawMessage(`{"subagents":{"agents":[{"name":"reader","agentSnapshot":{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true,"permission_policy":{"type":"always_allow"}}]}],"mcp_servers":[]}}]}}`)}
	record := db.CodeSession{Metadata: json.RawMessage(`{"config":{"tools":[],"mcp_servers":[]}}`)}
	result, err := dshChildContexts(record, snapshot, MCPRuntimeIdentity{Protocol: "dsr1", CodeSessionID: "session", SessionIngressToken: "new-owner"}, config.TunnelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result["reader"].ToolPermissions["read"] != "allow" {
		t.Fatalf("permissions=%+v", result)
	}
}

func TestDSHRegistrationRendersFrozenConfigurationWithCurrentCredentials(t *testing.T) {
	snapshot := dshContextSnapshot{WorkDir: "/original", Model: "original-model", Config: runtimeMCPTestSource("sse")}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	record := db.CodeSession{ExternalID: "session", WorkDir: "/changed", Model: "changed", Metadata: json.RawMessage(`{"config":{"mcp_servers":[]}}`)}
	cfg := config.Config{Tunnel: runtimeMCPTestConfig()}
	cfg.CodeSession.SandboxAPIBaseURL = "https://gateway.example"
	for _, token := range []string{"owner-one", "owner-two"} {
		encoded, err := renderDSHContext(record, raw, token, cfg)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Model     string `json:"model"`
			WorkDir   string `json:"cwd"`
			MCPConfig struct {
				Servers map[string]struct {
					Headers map[string]string `json:"headers"`
				} `json:"mcpServers"`
			} `json:"mcp_config"`
		}
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		if result.Model != "original-model" || result.WorkDir != "/original" || result.MCPConfig.Servers["local"].Headers["Authorization"] != "Bearer "+token {
			t.Fatalf("configuration or credential changed incorrectly: model=%s cwd=%s", result.Model, result.WorkDir)
		}
	}
	var frozen map[string]json.RawMessage
	if err := json.Unmarshal(raw, &frozen); err != nil {
		t.Fatal(err)
	}
	if _, found := frozen["session_token"]; found {
		t.Fatal("runtime credential stored in snapshot")
	}
}
