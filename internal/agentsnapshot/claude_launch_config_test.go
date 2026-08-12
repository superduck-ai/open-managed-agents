package agentsnapshot

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestClaudeLaunchConfigFromSnapshotRejectsMalformedSnapshot(t *testing.T) {
	t.Parallel()

	if _, err := ClaudeLaunchConfigFromSnapshot(json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed snapshot was accepted")
	}
}

func TestClaudeLaunchConfigFromSnapshotDerivesToolsAndPermissions(t *testing.T) {
	t.Parallel()

	config, err := ClaudeLaunchConfigFromSnapshot(json.RawMessage(`{
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
			},
			{"type":"mcp_toolset","mcp_server_name":"unsafe Read","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}},
			{"type":"mcp_toolset","mcp_server_name":"unsafe__Read","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}
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
	for _, tool := range allowed {
		if strings.HasPrefix(tool, "mcp__unsafe") {
			t.Fatalf("allowed tools %v contains an injected rule fragment", allowed)
		}
	}
}

func TestClaudeLaunchConfigFromSnapshotDerivesMCPConfig(t *testing.T) {
	t.Parallel()

	config, err := ClaudeLaunchConfigFromSnapshot(json.RawMessage(`{
		"mcp_servers":[{"type":"url","name":"notion","url":"https://mcp.notion.com/sse"}],
		"tools":[{
			"type":"mcp_toolset",
			"mcp_server_name":"notion",
			"configs":[
				{"name":"search","enabled":true,"permission_policy":{"type":"always_allow"}},
				{"name":"delete_page","enabled":false,"permission_policy":{"type":"always_ask"}}
			]
		}]
	}`))
	if err != nil {
		t.Fatalf("derive launch config: %v", err)
	}
	want := map[string]any{"mcpServers": map[string]any{
		"notion": map[string]any{
			"type": "sse",
			"url":  "https://mcp.notion.com/sse",
			"tools": []any{
				map[string]any{"name": "search", "enabled": true, "permission_policy": "allow"},
				map[string]any{"name": "delete_page", "enabled": false, "permission_policy": "ask"},
			},
		},
	}}
	if !reflect.DeepEqual(config.MCPConfig, want) {
		t.Fatalf("MCP config = %#v, want %#v", config.MCPConfig, want)
	}
}
