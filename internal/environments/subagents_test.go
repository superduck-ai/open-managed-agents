package environments

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestManagedAgentSubagentsRejectsInvalidNames(t *testing.T) {
	for _, source := range []string{
		`{"multiagent":{"agents":[{"id":"agent_a","model":{"id":"model"}}]}}`,
		`{"multiagent":{"agents":[{"id":"agent_a","name":"-reviewer","model":{"id":"model"}}]}}`,
		`{"multiagent":{"agents":[{"id":"agent_a","name":"reviewer","model":{"id":"model"}},{"id":"agent_b","name":"reviewer","model":{"id":"model"}}]}}`,
	} {
		if _, err := buildManagedAgentSubagents(json.RawMessage(source), nil); err == nil {
			t.Fatalf("accepted invalid child names: %s", source)
		}
	}
}

func TestManagedAgentSubagentsPreservesNameAndDescription(t *testing.T) {
	for _, test := range []struct {
		name, description, want string
	}{
		{name: "计算Agent", description: "内置核心工具集的空白起点。", want: "内置核心工具集的空白起点。"},
		{name: "计算Agent", want: "计算Agent"},
		{name: "Explore", description: "Calculate results", want: "Calculate results"},
	} {
		t.Run(test.want, func(t *testing.T) {
			source := json.RawMessage(fmt.Sprintf(`{"multiagent":{"agents":[{"id":"agent_child","name":%q,"description":%q,"model":{"id":"child-model"}}]}}`, test.name, test.description))
			raw, err := buildManagedAgentSubagents(source, nil)
			if err != nil {
				t.Fatal(err)
			}
			var encoded string
			if err := jsonv2.Unmarshal(mcpConfigField(t, raw, "claude_code_args", "agents"), &encoded); err != nil {
				t.Fatal(err)
			}
			var definitions map[string]claudeAgentDefinition
			if err := jsonv2.Unmarshal([]byte(encoded), &definitions); err != nil {
				t.Fatal(err)
			}
			definition, found := definitions[test.name]
			if !found || len(definitions) != 1 || definition.Description != test.want || strings.Contains(encoded, "agent_child") {
				t.Fatalf("registered by name=%t, count=%d, description=%q, want %q", found, len(definitions), definition.Description, test.want)
			}
		})
	}
}

func TestManagedAgentSubagentsUsesSessionMCPGateway(t *testing.T) {
	source := json.RawMessage(`{"mcp_servers":[{"name":"local","type":"url","url":"https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"}],"multiagent":{"agents":[{"id":"agent_child","name":"child","model":{"id":"child-model"},"mcp_servers":[{"name":"child_only","type":"url","url":"https://child.example/mcp"}]}]}}`)
	cfg := mcpConfigTestRuntimeConfig()
	raw, err := buildManagedAgentRuntimeMCPConfig(source, "cse_test", "runtime-token", cfg)
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
	if err := jsonv2.Unmarshal(mcpConfigField(t, definitions["child"].MCPServers[0]["local"], "url"), &address); err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if target.RawQuery != "" || len(definitions["child"].MCPServers) != 1 || strings.Contains(encoded, "child_only") || strings.Contains(encoded, "agent_child") {
		t.Fatalf("child Tunnel does not use session MCP scope: %s", address)
	}
}

func TestManagedAgentSubagentSkillsPreserveVersions(t *testing.T) {
	snapshot := json.RawMessage(`{"multiagent":{"agents":[{"id":"agent_a","name":"review-a","model":{"id":"model"},"skills":[{"type":"anthropic","skill_id":"review","version":"1"}]},{"id":"agent_b","name":"review-b","model":{"id":"model"},"skills":[{"type":"anthropic","skill_id":"review","version":"2"}]},{"id":"agent_c","name":"review-c","model":{"id":"model"},"skills":[{"type":"anthropic","skill_id":"review"}]}]}}`)
	resolved := []skills.RuntimeSkill{
		{Source: "anthropic", SkillID: "review", Version: "1", RequestedVersion: "1", Directory: "review-v1"},
		{Source: "anthropic", SkillID: "review", Version: "2", RequestedVersion: "latest", Directory: "review-v2"},
	}
	launch, err := managedAgentSessionConfig(db.Session{AgentSnapshot: snapshot}, managedAgentRuntimeResources{}, resolved)
	if err != nil {
		t.Fatal(err)
	}
	launch, err = buildManagedAgentSubagents(launch, nil)
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
	if !strings.HasSuffix(definitions["review-a"].Prompt, "\n\n"+mustEnvironmentPrompt(t, nil)) {
		t.Fatalf("subagent prompt lacks rendered environment prompt: %q", definitions["review-a"].Prompt)
	}
	for name, directory := range map[string]string{"review-a": "review-v1", "review-b": "review-v2", "review-c": "review-v2"} {
		if !slices.Equal(definitions[name].Skills, []string{directory}) {
			t.Errorf("%s skills=%v, want [%s]", name, definitions[name].Skills, directory)
		}
	}
}
