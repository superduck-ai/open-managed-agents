package agentconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestParseSessionAgentRejectsInvalidReferences(t *testing.T) {
	testCases := []struct {
		name      string
		raw       string
		wantError string
	}{
		{name: "missing", raw: ``, wantError: "agent is required"},
		{name: "null", raw: `null`, wantError: "agent is required"},
		{name: "empty string", raw: `""`, wantError: "agent id must be non-empty"},
		{name: "array", raw: `[]`, wantError: "agent must be a string or object"},
		{name: "unknown type", raw: `{"type":"deployment","id":"agent_1"}`, wantError: "agent.type must be agent or agent_with_overrides"},
		{name: "pinned with overrides", raw: `{"type":"agent","id":"agent_1","model":{"id":"claude-sonnet-4-6"}}`, wantError: "agent override fields require type agent_with_overrides"},
		{name: "overrides missing id", raw: `{"type":"agent_with_overrides"}`, wantError: "agent id must be non-empty"},
		{name: "overrides version zero", raw: `{"type":"agent_with_overrides","id":"agent_1","version":0}`, wantError: "agent.version must be at least 1"},
		{name: "overrides name", raw: `{"type":"agent_with_overrides","id":"agent_1","name":"x"}`, wantError: "agent_with_overrides cannot set name, description, metadata, or multiagent"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseSessionAgent(json.RawMessage(testCase.raw))
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("ParseSessionAgent() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestParseSessionAgentAcceptsUnionForms(t *testing.T) {
	stringRef, err := ParseSessionAgent(json.RawMessage(`" agent_1 "`))
	if err != nil {
		t.Fatal(err)
	}
	if stringRef.ID != "agent_1" || stringRef.Version != 0 || stringRef.WithOverrides {
		t.Fatalf("string ref = %#v", stringRef)
	}

	pinned, err := ParseSessionAgent(json.RawMessage(`{"type":"agent","id":"agent_1","version":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if pinned.ID != "agent_1" || pinned.Version != 3 || pinned.WithOverrides {
		t.Fatalf("pinned = %#v", pinned)
	}

	overridden, err := ParseSessionAgent(json.RawMessage(`{"type":"agent_with_overrides","id":"agent_1","system":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if overridden.ID != "agent_1" || !overridden.WithOverrides || string(overridden.Overrides.System) != "null" {
		t.Fatalf("overridden = %#v", overridden)
	}
}

func TestApplyRejectsInvalidOverrides(t *testing.T) {
	system := "you are a researcher"
	base := Config{
		Model:      json.RawMessage(`{"id":"claude-opus-4-6","speed":"fast"}`),
		System:     &system,
		Tools:      json.RawMessage(`[{"type":"agent_toolset_20260401","configs":[],"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]`),
		MCPServers: json.RawMessage(`[]`),
		Skills:     json.RawMessage(`[{"type":"anthropic","skill_id":"xlsx","version":"latest"}]`),
	}
	testCases := []struct {
		name      string
		overrides Overrides
		wantError string
	}{
		{
			name:      "null model",
			overrides: Overrides{Model: json.RawMessage(`null`)},
			wantError: "model cannot be null",
		},
		{
			name:      "unknown model",
			overrides: Overrides{Model: json.RawMessage(`{"id":"not-configured"}`)},
			wantError: `model "not-configured" is not configured for this workspace`,
		},
		{
			name:      "clear tools with skills",
			overrides: Overrides{Tools: json.RawMessage(`[]`)},
			wantError: "skills require the read tool",
		},
		{
			name:      "replace tools without read",
			overrides: Overrides{Tools: json.RawMessage(`[{"type":"mcp_toolset","mcp_server_name":"linear"}]`), MCPServers: json.RawMessage(`[{"name":"linear","type":"url","url":"https://mcp.linear.app/mcp"}]`)},
			wantError: "skills require the read tool",
		},
		{
			name:      "disable read with skills",
			overrides: Overrides{Tools: json.RawMessage(`[{"type":"agent_toolset_20260401","configs":[{"name":"read","enabled":false}]}]`)},
			wantError: "skills require the read tool",
		},
		{
			name:      "default config disables read with skills",
			overrides: Overrides{Tools: json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false}}]`)},
			wantError: "skills require the read tool",
		},
		{
			name:      "clear mcp servers while toolset remains",
			overrides: Overrides{MCPServers: json.RawMessage(`[]`), Tools: json.RawMessage(`[{"type":"mcp_toolset","mcp_server_name":"linear"}]`)},
			wantError: "mcp_toolset.mcp_server_name must reference an MCP server",
		},
	}
	allowed := []string{"claude-opus-4-6", "claude-sonnet-4-6"}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Apply(base, testCase.overrides, allowed)
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("Apply() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestOverridesReplacesModel(t *testing.T) {
	testCases := []struct {
		name      string
		overrides Overrides
		want      bool
	}{
		{name: "omit", overrides: Overrides{System: json.RawMessage(`null`)}},
		{name: "null", overrides: Overrides{Model: json.RawMessage(`null`)}},
		{name: "replace", overrides: Overrides{Model: json.RawMessage(`{"id":"claude-sonnet-4-6"}`)}, want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.overrides.ReplacesModel(); got != testCase.want {
				t.Fatalf("ReplacesModel() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestApplyDoesNotRevalidateUntouchedSkillsAndTools(t *testing.T) {
	system := "you are a researcher"
	base := configWithSkillsOnly()
	base.System = &system

	got, err := Apply(base, Overrides{System: json.RawMessage(`null`)}, []string{"unrelated-model"})
	if err != nil {
		t.Fatal(err)
	}
	if got.System != nil {
		t.Fatalf("system = %v, want nil", got.System)
	}
	if string(got.Model) != string(base.Model) {
		t.Fatalf("model changed: %s", got.Model)
	}
	if string(got.Tools) != `[]` || string(got.Skills) != string(base.Skills) {
		t.Fatalf("tools/skills changed: tools=%s skills=%s", got.Tools, got.Skills)
	}
}

func TestApplyAllowsReadFromDefaultToolsetConfig(t *testing.T) {
	base := configWithSkillsOnly()

	t.Run("default toolset enables read", func(t *testing.T) {
		got, err := Apply(base, Overrides{Tools: json.RawMessage(`[{"type":"agent_toolset_20260401"}]`)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got.Tools), `"type":"agent_toolset_20260401"`) {
			t.Fatalf("tools = %s", got.Tools)
		}
	})

	t.Run("explicit read overrides disabled default", func(t *testing.T) {
		got, err := Apply(base, Overrides{Tools: json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]`)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got.Tools), `"name":"read"`) {
			t.Fatalf("tools = %s", got.Tools)
		}
	})
}

func TestApplyReplacesFieldsWithoutMerging(t *testing.T) {
	system := "you are a researcher"
	base := Config{
		Model:      json.RawMessage(`{"id":"claude-opus-4-6","speed":"fast"}`),
		System:     &system,
		Tools:      json.RawMessage(`[{"type":"agent_toolset_20260401","configs":[{"enabled":true,"name":"bash","permission_policy":{"type":"always_allow"}}],"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]`),
		MCPServers: json.RawMessage(`[]`),
		Skills:     json.RawMessage(`[{"type":"anthropic","skill_id":"xlsx","version":"latest"}]`),
	}

	got, err := Apply(base, Overrides{
		Model:  json.RawMessage(`{"id":"claude-sonnet-4-6"}`),
		System: json.RawMessage(`null`),
		Skills: json.RawMessage(`[]`),
	}, []string{"claude-opus-4-6", "claude-sonnet-4-6"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Model) != `{"id":"claude-sonnet-4-6","speed":"standard"}` {
		t.Fatalf("model = %s", got.Model)
	}
	if got.System != nil {
		t.Fatalf("system = %v, want nil", got.System)
	}
	if string(got.Tools) != string(base.Tools) {
		t.Fatalf("tools changed: %s", got.Tools)
	}
	if string(got.Skills) != `[]` {
		t.Fatalf("skills = %s", got.Skills)
	}
}

func TestPatchSessionSnapshotRejectsFrozenFields(t *testing.T) {
	snapshot := json.RawMessage(`{"id":"agent_1","type":"agent","version":1,"model":{"id":"claude-opus-4-6","speed":"standard"},"system":"hi","tools":[],"mcp_servers":[],"skills":[]}`)
	testCases := []struct {
		name string
		raw  string
	}{
		{name: "type", raw: `{"type":"agent"}`},
		{name: "id", raw: `{"id":"agent_2"}`},
		{name: "version", raw: `{"version":2}`},
		{name: "model", raw: `{"model":{"id":"claude-sonnet-4-6"}}`},
		{name: "system", raw: `{"system":null}`},
		{name: "skills", raw: `{"skills":[]}`},
		{name: "name", raw: `{"name":"other"}`},
		{name: "description", raw: `{"description":"other"}`},
		{name: "metadata", raw: `{"metadata":{}}`},
		{name: "multiagent", raw: `{"multiagent":null}`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := PatchSessionSnapshot(snapshot, json.RawMessage(testCase.raw))
			if err == nil || !strings.Contains(err.Error(), "session agent updates may only replace tools and mcp_servers") {
				t.Fatalf("PatchSessionSnapshot() error = %v", err)
			}
		})
	}
}

func TestPatchSessionSnapshotReplacesTools(t *testing.T) {
	snapshot := json.RawMessage(`{"id":"agent_1","name":"Researcher","type":"agent","version":2,"model":{"id":"claude-opus-4-6","speed":"fast"},"system":"hi","tools":[{"type":"agent_toolset_20260401"}],"mcp_servers":[],"skills":[]}`)
	got, err := PatchSessionSnapshot(snapshot, json.RawMessage(`{"tools":[],"mcp_servers":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(got, &object); err != nil {
		t.Fatal(err)
	}
	if object["id"] != "agent_1" || object["version"] != float64(2) || object["name"] != "Researcher" {
		t.Fatalf("identity changed: %#v", object)
	}
	tools, _ := object["tools"].([]any)
	if len(tools) != 0 {
		t.Fatalf("tools = %#v", object["tools"])
	}
}

func TestPatchSessionSnapshotRejectsToolsWithoutReadWhenSkillsRemain(t *testing.T) {
	snapshot := json.RawMessage(`{"id":"agent_1","type":"agent","version":1,"model":{"id":"claude-opus-4-6","speed":"standard"},"system":null,"tools":[{"type":"agent_toolset_20260401"}],"mcp_servers":[],"skills":[{"type":"anthropic","skill_id":"xlsx","version":"latest"}]}`)
	_, err := PatchSessionSnapshot(snapshot, json.RawMessage(`{"tools":[]}`))
	if err == nil || !strings.Contains(err.Error(), "skills require the read tool") {
		t.Fatalf("PatchSessionSnapshot() error = %v", err)
	}
}

func TestPatchSessionSnapshotAllowsMCPServersWithoutRecheckingRead(t *testing.T) {
	snapshot := json.RawMessage(`{"id":"agent_1","type":"agent","version":1,"model":{"id":"claude-opus-4-6","speed":"standard"},"system":null,"tools":[],"mcp_servers":[],"skills":[{"type":"anthropic","skill_id":"xlsx","version":"latest"}]}`)
	got, err := PatchSessionSnapshot(snapshot, json.RawMessage(`{"mcp_servers":[{"name":"github","type":"url","url":"https://mcp.github.example/mcp"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"name":"github"`) {
		t.Fatalf("snapshot = %s", got)
	}
	if !strings.Contains(string(got), `"skill_id":"xlsx"`) {
		t.Fatalf("skills dropped: %s", got)
	}
}

func TestWriteAgentKeepsIdentity(t *testing.T) {
	system := "original"
	agent := db.Agent{
		ExternalID:     "agent_1",
		CurrentVersion: 4,
		Name:           "Lead",
		System:         &system,
		Model:          json.RawMessage(`{"id":"claude-opus-4-6","speed":"fast"}`),
		Tools:          json.RawMessage(`[]`),
		MCPServers:     json.RawMessage(`[]`),
		Skills:         json.RawMessage(`[]`),
	}
	cleared := Config{
		Model:      json.RawMessage(`{"id":"claude-sonnet-4-6","speed":"standard"}`),
		Tools:      json.RawMessage(`[]`),
		MCPServers: json.RawMessage(`[]`),
		Skills:     json.RawMessage(`[]`),
	}
	got := WriteAgent(agent, cleared)
	if got.ExternalID != "agent_1" || got.CurrentVersion != 4 || got.Name != "Lead" {
		t.Fatalf("identity = %#v", got)
	}
	if got.System != nil {
		t.Fatalf("system = %v", got.System)
	}
	if string(got.Model) != string(cleared.Model) {
		t.Fatalf("model = %s", got.Model)
	}
}

func configWithSkillsOnly() Config {
	return Config{
		Model:      json.RawMessage(`{"id":"claude-opus-4-6","speed":"fast"}`),
		Tools:      json.RawMessage(`[]`),
		MCPServers: json.RawMessage(`[]`),
		Skills:     json.RawMessage(`[{"type":"anthropic","skill_id":"xlsx","version":"latest"}]`),
	}
}
