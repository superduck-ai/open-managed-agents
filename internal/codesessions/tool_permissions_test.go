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
