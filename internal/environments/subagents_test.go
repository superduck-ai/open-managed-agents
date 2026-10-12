package environments

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
	"net/url"
	"slices"
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

func TestManagedAgentSubagentSkillsPreserveVersions(t *testing.T) {
	for _, source := range []string{"custom", "anthropic"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reverse=%t", source, reverse), func(t *testing.T) {
				snapshot := json.RawMessage(fmt.Sprintf(`{"multiagent":{"agents":[{"id":"agent_a","model":{"id":"model"},"skills":[{"type":%q,"skill_id":"review","version":"1"}]},{"id":"agent_b","model":{"id":"model"},"skills":[{"type":%q,"skill_id":"review","version":"2"}]},{"id":"agent_c","model":{"id":"model"},"skills":[{"type":%q,"skill_id":"review"}]}]}}`, source, source, source))
				resolved := []skills.RuntimeSkill{
					{Source: source, SkillID: "review", Version: "1", RequestedVersion: "1", Directory: "review-v1"},
					{Source: source, SkillID: "review", Version: "2", RequestedVersion: "latest", Directory: "review-v2"},
				}
				if reverse {
					slices.Reverse(resolved)
				}
				launch, err := managedAgentSessionConfig(db.Session{AgentSnapshot: snapshot}, managedAgentRuntimeResources{}, resolved)
				if err != nil {
					t.Fatal(err)
				}
				launch, err = buildManagedAgentSubagents(launch, codesessions.MCPRuntimeIdentity{}, mcpConfigTestRuntimeConfig().Tunnel)
				if err != nil {
					t.Fatal(err)
				}
				var encoded string
				if err := jsonv2.Unmarshal(mcpConfigField(t, launch, "claude_code_args", "agents"), &encoded); err != nil {
					t.Fatal(err)
				}
				var definitions map[string]claudeAgentDefinition
				if err := jsonv2.Unmarshal([]byte(encoded), &definitions); err != nil {
					t.Fatal(err)
				}
				for id, directory := range map[string]string{"agent_a": "review-v1", "agent_b": "review-v2", "agent_c": "review-v2"} {
					if !slices.Equal(definitions[id].Skills, []string{directory}) {
						t.Errorf("%s skills=%v, want [%s]", id, definitions[id].Skills, directory)
					}
				}
			})
		}
	}
}
