package codesessions

import (
	"encoding/json"
	"testing"
)

func TestResolveToolPermissionFromAgentSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot string
		toolName string
		want     resolvedToolPermission
	}{
		{
			name: "mcp default allow applies without explicit configs",
			snapshot: `{
				"tools":[{
					"type":"mcp_toolset",
					"mcp_server_name":"weather_service",
					"configs":[],
					"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}
				}]
			}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionAllow,
		},
		{
			name: "mcp config overrides default",
			snapshot: `{
				"tools":[{
					"type":"mcp_toolset",
					"mcp_server_name":"weather_service",
					"configs":[{"name":"delete_weather","enabled":false,"permission_policy":{"type":"always_allow"}}],
					"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}
				}]
			}`,
			toolName: "mcp__weather_service__delete_weather",
			want:     resolvedToolPermissionDeny,
		},
		{
			name: "missing mcp toolset defaults to ask",
			snapshot: `{
				"tools":[{"type":"agent_toolset_20260401"}]
			}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "agent toolset missing defaults to allow",
			snapshot: `{"tools":[]}`,
			toolName: "Bash",
			want:     resolvedToolPermissionAllow,
		},
		{
			name: "agent toolset config overrides default",
			snapshot: `{
				"tools":[{
					"type":"agent_toolset_20260401",
					"configs":[{"name":"bash","enabled":true,"permission_policy":{"type":"always_ask"}}],
					"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}
				}]
			}`,
			toolName: "Bash",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "unknown tool defaults to ask",
			snapshot: `{"tools":[]}`,
			toolName: "MysteryTool",
			want:     resolvedToolPermissionAsk,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resolveToolPermissionFromAgentSnapshot(json.RawMessage(tt.snapshot), tt.toolName)
			if got != tt.want {
				t.Fatalf("permission = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseClaudeToolIdentity(t *testing.T) {
	t.Parallel()

	identity := parseClaudeToolIdentity("mcp__weather_service__get_weather")
	if identity.Kind != "mcp" || identity.ServerName != "weather_service" || identity.ToolName != "get_weather" {
		t.Fatalf("identity = %+v", identity)
	}

	identity = parseClaudeToolIdentity("MultiEdit")
	if identity.Kind != "agent_toolset" || identity.ToolName != "edit" {
		t.Fatalf("identity = %+v", identity)
	}
}
