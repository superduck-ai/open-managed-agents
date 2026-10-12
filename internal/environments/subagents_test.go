package environments

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"net/url"
	"testing"
)

func TestManagedAgentSubagentsUsesChildMCPGateway(t *testing.T) {
	source := json.RawMessage(`{"multiagent":{"agents":[{"id":"agent_child","name":"child","model":{"id":"child-model"},"mcp_servers":[{"name":"local","type":"url","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}]}]}}`)
	cfg := mcpConfigTestRuntimeConfig()
	raw, err := buildManagedAgentSubagents(source, codesessions.MCPRuntimeIdentity{APIBaseURL: "https://runtime.example", CodeSessionID: "cse_test", SessionIngressToken: "runtime-token"}, cfg.Tunnel)
	if err != nil {
		t.Fatal(err)
	}
	var encoded string
	if err := jsonv2.Unmarshal(mcpConfigField(t, raw, "claude_code_args", "agents"), &encoded); err != nil {
		t.Fatal(err)
	}
	var address string
	var definitions map[string]claudeAgentDefinition
	if err := jsonv2.Unmarshal([]byte(encoded), &definitions); err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal(mcpConfigField(t, definitions["agent_child"].MCPServers[0]["local"], "url"), &address); err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("config_agent_id") != "agent_child" {
		t.Fatalf("child Tunnel uses parent MCP scope: %s", address)
	}
}
