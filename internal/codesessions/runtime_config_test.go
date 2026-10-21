package codesessions

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestManagedAgentRuntimeConfigRejectsInvalidKnownFields(t *testing.T) {
	service := &Service{}
	for _, source := range []string{
		`{"system_prompt":42}`,
		`{"runtime_skills":{}}`,
		`{"subagents":{"agents":"invalid"}}`,
	} {
		if _, err := service.managedAgentRuntimeConfig(t.Context(), db.Session{}, json.RawMessage(source)); err == nil {
			t.Fatalf("accepted invalid runtime config: %s", source)
		}
	}
}

func TestManagedAgentRuntimeConfigPreservesExtensionsAndPinsSubagents(t *testing.T) {
	service := &Service{}
	session := db.Session{AgentSnapshot: json.RawMessage(`{"id":"parent","version":1,"name":"reader","system":"pinned","multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"parent","version":1}]},"tools":[]}`)}
	source := json.RawMessage(`{"system_prompt":"instructions","runtime_skills":[],"extension":{"precise":9007199254740993},"subagents":{"agents":[]}}`)
	result, err := service.managedAgentRuntimeConfig(t.Context(), session, source)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(result, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["extension"]) != `{"precise":9007199254740993}` || string(fields["system_prompt"]) != `"instructions"` || string(fields["runtime_skills"]) != `[]` {
		t.Fatalf("runtime config fields changed: %s", result)
	}
	var subagents RuntimeSubagentConfig
	if err := json.Unmarshal(fields["subagents"], &subagents); err != nil {
		t.Fatal(err)
	}
	if len(subagents.Agents) != 1 || subagents.Agents[0].Name != "reader" || subagents.Agents[0].SystemPrompt != "pinned" {
		t.Fatalf("subagents = %+v", subagents)
	}
}

func TestRuntimeRootToolsRejectsInvalidSnapshot(t *testing.T) {
	if _, err := runtimeRootToolsets(json.RawMessage(`{"tools":"invalid"}`)); err == nil {
		t.Fatal("invalid tool snapshot accepted")
	}
}

func TestRuntimeRootToolsMatchPermissionEnforcement(t *testing.T) {
	all := []string{"Bash", "Read", "Write", "Edit", "Grep", "Glob"}
	for snapshot, want := range map[string][]string{
		``:                   all,
		`{"system":"plain"}`: all,
		`{"tools":[]}`:       all,
		`{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true},{"name":"grep","enabled":true}]}]}`:                           {"Read", "Grep"},
		`{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","enabled":false},{"name":"edit","enabled":false},{"name":"bash","permission_policy":{"type":"always_ask"}}]}]}`: {"Bash", "Read", "Grep", "Glob"},
	} {
		tools, err := runtimeRootToolsets(json.RawMessage(snapshot))
		if got := enabledSandboxTools(tools); err != nil || !slices.Equal(got, want) {
			t.Errorf("enabledSandboxTools(%s) = %v, %v; want %v", snapshot, got, err, want)
		}
	}
}

func TestManagedAgentRuntimeConfigPublishesRootTools(t *testing.T) {
	service := &Service{}
	session := db.Session{AgentSnapshot: json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]}`)}
	result, err := service.managedAgentRuntimeConfig(t.Context(), session, json.RawMessage(`{"system_prompt":"instructions"}`))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(result, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["runtime_tools"]) != `["Read"]` {
		t.Fatalf("runtime_tools = %s", fields["runtime_tools"])
	}
	want := `{"bash":"deny","edit":"deny","glob":"deny","grep":"deny","read":"allow","write":"deny"}`
	if string(fields["runtime_tool_permissions"]) != want {
		t.Fatalf("runtime_tool_permissions = %s", fields["runtime_tool_permissions"])
	}
}

func TestRuntimeToolPoliciesMatchPermissionEnforcement(t *testing.T) {
	snapshot := json.RawMessage(`{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","enabled":false},{"name":"bash","permission_policy":{"type":"always_ask"}}]}]}`)
	tools, err := runtimeRootToolsets(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	policies := runtimeToolPolicies(tools)
	for _, name := range sandboxToolNames {
		permission, _ := resolveToolPermissionFromAgentSnapshot(snapshot, name)
		if remoteName := managedAgentToolName(name); policies[remoteName] != string(permission) {
			t.Errorf("%s policy = %q, enforcement = %q", remoteName, policies[remoteName], permission)
		}
	}
	if policies["bash"] != "ask" || policies["write"] != "deny" || policies["read"] != "allow" {
		t.Fatalf("policies = %v", policies)
	}
}
