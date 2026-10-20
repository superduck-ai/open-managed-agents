package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestSessionMCPOverrideReferenceAcceptsPublicField(t *testing.T) {
	var ref sessionAgentReference
	raw := `{"type":"agent_with_overrides","id":"agent_fixture","version":3,"mcp_servers":[{"type":"url","name":"peer","url":"https://peer.test/mcp/b"}]}`
	if err := json.Unmarshal([]byte(raw), &ref); err != nil {
		t.Fatal(err)
	}
	if err := ref.validateType(); err != nil {
		t.Fatalf("public MCP override rejected: %v", err)
	}
}

func mcpOverrideFixtureAgent() db.Agent {
	return db.Agent{ExternalID: "agent_fixture", CurrentVersion: 3, Tools: json.RawMessage(`[{"type":"mcp_toolset","mcp_server_name":"peer","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]`), MCPServers: json.RawMessage(`[{"type":"url","name":"peer","url":"https://peer.test/mcp/a"}]`), Skills: json.RawMessage(`[]`)}
}

func TestSessionMCPOverrideValidatesEffectiveReferences(t *testing.T) {
	base := mcpOverrideFixtureAgent()
	for _, raw := range []string{`null`, `[]`, `[{"type":"url","name":"different","url":"https://peer.test/mcp/b"}]`, `[{"type":"url","name":"peer","url":"http://user:secret@peer.test/mcp"}]`, `[{"type":"url","name":"peer","url":"https://peer.test/mcp#fragment"}]`} {
		if _, err := applySessionToolConfigurationOverrides(base, nil, json.RawMessage(raw)); err == nil {
			t.Fatal("invalid effective MCP accepted", raw)
		}
	}
}

func TestSessionMCPOverrideReplacesOnlyEffectiveCopy(t *testing.T) {
	base := mcpOverrideFixtureAgent()
	original := string(base.MCPServers)
	servers := json.RawMessage(`[{"type":"url","name":"peer","url":"https://peer.test/mcp/b"}]`)
	effective, err := applySessionToolConfigurationOverrides(base, nil, servers)
	if err != nil {
		t.Fatal(err)
	}
	if effective.ExternalID != base.ExternalID || effective.CurrentVersion != base.CurrentVersion || string(effective.Tools) != string(base.Tools) || string(base.MCPServers) != original {
		t.Fatal("source configuration or identity changed")
	}
	var got []struct {
		URL string `json:"url"`
	}
	json.Unmarshal(effective.MCPServers, &got)
	if len(got) != 1 || got[0].URL != "https://peer.test/mcp/b" {
		t.Fatal("new endpoint lost", string(effective.MCPServers))
	}
	inherited, err := applySessionToolConfigurationOverrides(base, nil, nil)
	if err != nil || string(inherited.MCPServers) != original {
		t.Fatal("omission did not inherit", err)
	}
}

func TestSessionMCPCombinedOverridesValidateFinalConfiguration(t *testing.T) {
	base := mcpOverrideFixtureAgent()
	cleared, err := applySessionToolConfigurationOverrides(base, json.RawMessage(`[]`), json.RawMessage(`null`))
	if err != nil || string(cleared.Tools) != "[]" || string(cleared.MCPServers) != "[]" {
		t.Fatal("valid combined clear rejected", err)
	}
	servers := json.RawMessage(`[{"type":"url","name":"second","url":"https://peer.test/mcp/b"}]`)
	tools := json.RawMessage(`[{"type":"mcp_toolset","mcp_server_name":"second","default_config":{"permission_policy":{"type":"always_allow"}}}]`)
	if _, err := applySessionToolConfigurationOverrides(base, tools, servers); err != nil {
		t.Fatal("validated intermediate instead of final tools/server pair", err)
	}
}
