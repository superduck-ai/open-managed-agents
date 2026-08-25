package codesessions

import (
	"encoding/json"
	"reflect"
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

func TestToolConfirmationUpdatedInput(t *testing.T) {
	t.Parallel()

	questions := []any{
		map[string]any{"header": "Color", "question": "Favorite color?"},
	}
	original := map[string]any{"questions": questions}

	tests := []struct {
		name         string
		original     map[string]any
		confirmation map[string]any
		want         map[string]any
	}{
		{
			name:         "nil original and confirmation returns empty object",
			original:     nil,
			confirmation: nil,
			want:         map[string]any{},
		},
		{
			name:         "non-object extras are ignored",
			original:     map[string]any{"path": "/tmp/example.txt"},
			confirmation: map[string]any{"result": "allow", "updated_input": "not-an-object", "answers": "Blue"},
			want:         map[string]any{"path": "/tmp/example.txt"},
		},
		{
			name:         "allow without extras copies original input",
			original:     map[string]any{"path": "/tmp/example.txt", "contents": "hello"},
			confirmation: map[string]any{"result": "allow"},
			want:         map[string]any{"path": "/tmp/example.txt", "contents": "hello"},
		},
		{
			name:     "updated_input replaces original input",
			original: original,
			confirmation: map[string]any{
				"updated_input": map[string]any{
					"questions": questions,
					"answers":   map[string]any{"Color": "Blue"},
				},
			},
			want: map[string]any{
				"questions": questions,
				"answers":   map[string]any{"Color": "Blue"},
			},
		},
		{
			name:     "answers overlay original questions",
			original: original,
			confirmation: map[string]any{
				"answers": map[string]any{"Color": "Blue"},
			},
			want: map[string]any{
				"questions": questions,
				"answers":   map[string]any{"Color": "Blue"},
			},
		},
		{
			name:     "answers overlay updated_input",
			original: original,
			confirmation: map[string]any{
				"updated_input": map[string]any{
					"questions": questions,
					"answers":   map[string]any{"Color": "Green"},
				},
				"answers": map[string]any{"Color": "Blue"},
			},
			want: map[string]any{
				"questions": questions,
				"answers":   map[string]any{"Color": "Blue"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toolConfirmationUpdatedInput(tt.original, tt.confirmation)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("updated input = %#v, want %#v", got, tt.want)
			}
			if _, ok := tt.original["answers"]; ok {
				t.Fatalf("original input was mutated with answers: %#v", tt.original)
			}
		})
	}
}
