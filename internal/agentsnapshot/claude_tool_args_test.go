package agentsnapshot

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestClaudeToolArgsFromSnapshotRejectsMalformedSnapshot(t *testing.T) {
	t.Parallel()

	if _, err := ClaudeToolArgsFromSnapshot(json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed snapshot was accepted")
	}
}

func TestClaudeToolArgsFromSnapshotDerivesToolsAndPermissions(t *testing.T) {
	t.Parallel()

	config, err := ClaudeToolArgsFromSnapshot(json.RawMessage(`{
		"tools":[
			{
				"type":"agent_toolset_20260401",
				"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}},
				"configs":[
					{"name":"bash","enabled":true,"permission_policy":{"type":"always_ask"}},
					{"name":"write","enabled":false,"permission_policy":{"type":"always_allow"}}
				]
			},
			{
				"type":"mcp_toolset",
				"mcp_server_name":"notion",
				"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}},
				"configs":[
					{"name":"search","enabled":true,"permission_policy":{"type":"always_allow"}},
					{"name":"delete_page","enabled":false,"permission_policy":{"type":"always_allow"}}
				]
			},
			{
				"type":"mcp_toolset",
				"mcp_server_name":"you",
				"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}},
				"configs":[]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("derive launch config: %v", err)
	}
	wantTools := []string{
		"Bash", "Read", "Write", "Edit", "Glob", "Grep", "WebFetch", "Task", "AskUserQuestion",
		"CronCreate", "CronDelete", "CronList", "EnterPlanMode", "EnterWorktree", "ExitPlanMode",
		"ExitWorktree", "NotebookEdit", "ScheduleWakeup", "Skill", "TaskOutput", "TaskStop", "TodoWrite",
	}
	if got := strings.Split(config.Tools, ","); !slices.Equal(got, wantTools) {
		t.Fatalf("tools = %#v, want %#v", got, wantTools)
	}
	allowed := strings.Split(config.AllowedTools, ",")
	for _, expected := range []string{"Task", "AskUserQuestion", "Read", "WebFetch", "mcp__notion__search", "mcp__you__*"} {
		if !slices.Contains(allowed, expected) {
			t.Fatalf("allowed tools %v missing %q", allowed, expected)
		}
	}
	for _, excluded := range []string{"Bash", "Write", "mcp__notion__delete_page", "mcp__notion__*"} {
		if slices.Contains(allowed, excluded) {
			t.Fatalf("allowed tools %v unexpectedly contains %q", allowed, excluded)
		}
	}
}

func TestClaudeToolArgsDefaultAllowWithAskOverrideOmitsMCPWildcard(t *testing.T) {
	t.Parallel()

	config, err := ClaudeToolArgsFromSnapshot(json.RawMessage(`{
		"tools":[{
			"type":"mcp_toolset",
			"mcp_server_name":"search",
			"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}},
			"configs":[{"name":"delete","enabled":true,"permission_policy":{"type":"always_ask"}}]
		}]
	}`))
	if err != nil {
		t.Fatalf("derive launch config: %v", err)
	}
	if config.AllowedTools != "" {
		t.Fatalf("allowed tools = %q, want no wildcard or explicit ask rule", config.AllowedTools)
	}
}
