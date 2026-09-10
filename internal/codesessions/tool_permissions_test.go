package codesessions

import (
	"encoding/json"
	"testing"
	"uuid"
)

func TestControlResponseUUIDMatchesExistingUUIDv5(t *testing.T) {
	t.Parallel()

	const want = "f2342556-f950-52b1-9f44-b34130c2bfd5"
	got := controlResponseUUID("codeses_test", "request_test")
	if got != want {
		t.Fatalf("control response UUID = %q, want %q", got, want)
	}
	parsed := uuid.MustParse(got)
	if version := parsed[6] >> 4; version != 5 {
		t.Fatalf("control response UUID version = %d, want 5", version)
	}
}

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

			got, _ := resolveToolPermissionFromAgentSnapshot(json.RawMessage(tt.snapshot), tt.toolName)
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

func TestResolveMCPToolPermissionForRuntimeNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot string
		toolName string
		want     resolvedToolPermission
	}{
		{
			name:     "malformed permission configuration does not allow",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","default_config":{"enabled":"false","permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "empty tool suffix does not allow",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather_service__",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "unknown runtime server still asks",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__other_service__get_weather",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "colliding runtime names cannot use the exact name allow policy",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","default_config":{"enabled":false}},{"type":"mcp_toolset","mcp_server_name":"weather_service","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "unconfigured server participates in collision detection",
			snapshot: `{"mcp_servers":[{"name":"weather.service"},{"name":"weather_service"}],"tools":[{"type":"mcp_toolset","mcp_server_name":"weather_service","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "overlapping server prefixes cannot borrow a longer server policy",
			snapshot: `{"mcp_servers":[{"name":"weather"},{"name":"weather..service"}],"tools":[{"type":"mcp_toolset","mcp_server_name":"weather..service","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather__service__get_weather",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "disabled dotted server tool overrides allow default",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","configs":[{"name":"delete_weather","enabled":false}],"default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather_service__delete_weather",
			want:     resolvedToolPermissionDeny,
		},
		{
			name:     "dotted server tool ask overrides allow default",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","configs":[{"name":"get_weather","permission_policy":{"type":"always_ask"}}],"default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionAsk,
		},
		{
			name:     "disabled dotted server default denies",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","default_config":{"enabled":false,"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionDeny,
		},
		{
			name:     "dotted tunnel server default allows",
			snapshot: `{"mcp_servers":[{"name":"tunnel_0123456789abcdef0123456789abcdef.main"}],"tools":[{"type":"mcp_toolset","mcp_server_name":"tunnel_0123456789abcdef0123456789abcdef.main","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__tunnel_0123456789abcdef0123456789abcdef_main__echo",
			want:     resolvedToolPermissionAllow,
		},
		{
			name:     "dotted server tool allow overrides ask default",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","configs":[{"name":"get_weather","permission_policy":{"type":"always_allow"}}],"default_config":{"permission_policy":{"type":"always_ask"}}}]}`,
			toolName: "mcp__weather_service__get_weather",
			want:     resolvedToolPermissionAllow,
		},
		{
			name:     "consecutive dots in server do not become the tool separator",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"tunnel_0123456789abcdef0123456789abcdef.internal..tools","configs":[{"name":"echo__text","permission_policy":{"type":"always_allow"}}]}]}`,
			toolName: "mcp__tunnel_0123456789abcdef0123456789abcdef_internal__tools__echo__text",
			want:     resolvedToolPermissionAllow,
		},
		{
			name:     "leading dots do not hide the MCP server",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"..weather","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp____weather__get_weather",
			want:     resolvedToolPermissionAllow,
		},
		{
			name:     "unchanged worker name remains supported",
			snapshot: `{"tools":[{"type":"mcp_toolset","mcp_server_name":"weather.service","default_config":{"permission_policy":{"type":"always_allow"}}}]}`,
			toolName: "mcp__weather.service__get_weather",
			want:     resolvedToolPermissionAllow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := resolveToolPermissionFromAgentSnapshot(json.RawMessage(tt.snapshot), tt.toolName)
			if got != tt.want {
				t.Fatalf("permission = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestToolPermissionPublicPayloadsUseCanonicalPublicID(t *testing.T) {
	t.Parallel()

	requestID := "request-weather"
	payload := workerControlRequestPayload{
		Request: workerPermissionRequest{
			ToolName:  "mcp__weather_service__get_weather",
			ToolUseID: "toolu_weather",
			Input:     map[string]any{"location": "Beijing"},
		},
	}
	request, publicPayloads, err := toolPermissionPublicPayloads(
		"cse_test",
		&payload,
		EventMetadata{RequestID: &requestID},
		parseClaudeToolIdentity(payload.Request.ToolName),
		resolvedToolPermissionAsk,
	)
	if err != nil {
		t.Fatalf("build public payloads: %v", err)
	}
	if len(publicPayloads) != 2 {
		t.Fatalf("public payload count = %d, want 2", len(publicPayloads))
	}
	var toolEvent map[string]any
	if err := json.Unmarshal(publicPayloads[0], &toolEvent); err != nil {
		t.Fatalf("decode tool event: %v", err)
	}
	if toolEvent["id"] != request.PublicEventID || toolEvent["type"] != "agent.mcp_tool_use" || toolEvent["name"] != "get_weather" || toolEvent["evaluated_permission"] != "ask" {
		t.Fatalf("canonical tool event = %#v", toolEvent)
	}
	for _, privateField := range []string{"content", "message", "tool_use_id", "request_id", "requires_action_details"} {
		if _, ok := toolEvent[privateField]; ok {
			t.Fatalf("canonical tool event leaked %s: %#v", privateField, toolEvent)
		}
	}
	var statusEvent map[string]any
	if err := json.Unmarshal(publicPayloads[1], &statusEvent); err != nil {
		t.Fatalf("decode status event: %v", err)
	}
	stopReason, _ := statusEvent["stop_reason"].(map[string]any)
	eventIDs, _ := stopReason["event_ids"].([]any)
	if len(eventIDs) != 1 || eventIDs[0] != request.PublicEventID {
		t.Fatalf("stop_reason.event_ids = %#v, want [%s]", eventIDs, request.PublicEventID)
	}
	if _, ok := statusEvent["requires_action_details"]; ok {
		t.Fatalf("status event leaked private action details: %#v", statusEvent)
	}
}
