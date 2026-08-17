package codesessions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestCodeSessionHTTPPollReturnsErrorsThroughAdapter(t *testing.T) {
	handler := NewHandler(config.Config{}, newTestService(t, nil), nil, nil)
	router := chi.NewRouter()
	handler.RegisterV1Routes(router)

	request := httptest.NewRequest(http.MethodGet, "/code/sessions/cse_test/", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestSessionContextFromCodeSessionUsesStoredConfig(t *testing.T) {
	record := db.CodeSession{
		WorkDir: "/workspace/repo",
		Model:   "claude-opus-4-8",
		Metadata: json.RawMessage(`{
			"config":{
				"outcomes":[{"type":"complete"}],
				"custom_system_prompt":"You are Codex.",
				"append_system_prompt":"Use MCP when useful.",
				"mcp_config":{"mcpServers":{"notion":{"type":"http","url":"https://mcp.notion.com/mcp"}}}
			}
		}`),
	}

	context := sessionContextFromCodeSession(record)
	if context["cwd"] != "/workspace/repo" || context["model"] != "claude-opus-4-8" {
		t.Fatalf("unexpected base context: %#v", context)
	}
	if context["custom_system_prompt"] != "You are Codex." || context["append_system_prompt"] != "Use MCP when useful." {
		t.Fatalf("unexpected prompts: %#v", context)
	}
	if len(context["outcomes"].([]any)) != 1 {
		t.Fatalf("unexpected outcomes: %#v", context["outcomes"])
	}
	mcpConfig := context["mcp_config"].(map[string]any)
	servers := mcpConfig["mcpServers"].(map[string]any)
	notion := servers["notion"].(map[string]any)
	if notion["type"] != "http" || notion["url"] != "https://mcp.notion.com/mcp" {
		t.Fatalf("unexpected mcp config: %#v", mcpConfig)
	}
}

func TestCodeSessionWorkerEventsBodyRetainsPayloadAndDecodesControlSchema(t *testing.T) {
	var request codeSessionWorkerEventsBody
	err := json.Unmarshal([]byte(`{
		"worker_epoch":"42",
		"events":[{
			"ephemeral":false,
			"payload":{
				"type":"control_request",
				"uuid":"event-uuid",
				"request_id":"request-id",
				"request":{
					"subtype":"can_use_tool",
					"tool_name":"Bash",
					"tool_use_id":"tool-use-id",
					"session_thread_id":"thread-id",
					"input":{"command":"go test ./..."}
				}
			}
		},{
			"payload":{"type":"keep_alive"}
		},{
			"payload":{
				"type":"assistant",
				"uuid":"assistant-uuid",
				"session_id":"session-uuid",
				"parent_tool_use_id":null,
				"message":{"role":"assistant","content":[]}
			}
		},{
			"payload":{
				"type":"system",
				"subtype":"task_started",
				"uuid":"system-uuid",
				"session_id":"session-uuid",
				"task_id":"task-id",
				"description":"inspect repository"
			}
		},{
			"ephemeral":true,
			"payload":{
				"type":"stream_event",
				"uuid":"stream-uuid",
				"session_id":"session-uuid",
				"parent_tool_use_id":null,
				"event":{"type":"content_block_delta"}
			}
		}]
	}`), &request)
	if err != nil {
		t.Fatalf("decode worker events: %v", err)
	}
	if request.WorkerEpoch != 42 {
		t.Fatalf("worker epoch = %d, want 42", request.WorkerEpoch)
	}
	if len(request.Events) != 5 {
		t.Fatalf("event count = %d, want 5", len(request.Events))
	}
	header, err := decodeWorkerPayloadHeader(request.Events[0].Payload)
	if err != nil {
		t.Fatalf("decode control request header: %v", err)
	}
	if header.Type != "control_request" || header.UUID != "event-uuid" {
		t.Fatalf("unexpected payload header: %#v", header)
	}
	payload, err := decodeWorkerControlRequestPayload(request.Events[0].Payload)
	if err != nil {
		t.Fatalf("decode control request payload: %v", err)
	}
	if payload.RequestID != "request-id" {
		t.Fatalf("unexpected payload schema: %#v", payload)
	}
	if payload.Request.Subtype != "can_use_tool" || payload.Request.ToolName != "Bash" || payload.Request.ToolUseID != "tool-use-id" {
		t.Fatalf("unexpected control request schema: %#v", payload.Request)
	}
	if payload.Request.Input["command"] != "go test ./..." {
		t.Fatalf("request input = %#v", payload.Request.Input)
	}
	if threadID := workerOutputSessionThreadID(&payload); threadID != "thread-id" {
		t.Fatalf("session thread id = %q, want thread-id", threadID)
	}
	for i, wantType := range []string{"control_request", "keep_alive", "assistant", "system", "stream_event"} {
		header, err := decodeWorkerPayloadHeader(request.Events[i].Payload)
		if err != nil || header.Type != wantType {
			t.Fatalf("events[%d] header = %#v, error = %v", i, header, err)
		}
	}
	if !request.Events[4].Ephemeral {
		t.Fatal("stream event must retain ephemeral flag")
	}
	encoded, err := json.Marshal(request.Events[0])
	if err != nil {
		t.Fatalf("encode worker event: %v", err)
	}
	var roundTrip struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("decode encoded worker event: %v", err)
	}
	if roundTrip.Payload["type"] != "control_request" || roundTrip.Payload["uuid"] != "event-uuid" {
		t.Fatalf("encoded payload = %#v", roundTrip.Payload)
	}

	if err := json.Unmarshal([]byte(`{"worker_epoch":42,"events":[{"payload":{"type":"assistant"}}]}`), &request); err != nil {
		t.Fatalf("envelope must retain incomplete payload: %v", err)
	}
	_, err = decodeWorkerPayloadHeader(request.Events[0].Payload)
	if err == nil || err.Error() != "payload.uuid is required" {
		t.Fatalf("missing uuid error = %v", err)
	}
}

func TestDecodeWorkerPayloadHeader(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantType string
		wantErr  string
	}{
		{name: "known", body: `{"type":"assistant","uuid":"event-uuid","message":{"future":true}}`, wantType: "assistant"},
		{name: "unknown", body: `{"type":"future_sdk_event","uuid":"event-uuid","future_field":true}`, wantType: "future_sdk_event"},
		{name: "keep alive without uuid", body: `{"type":"keep_alive"}`, wantType: "keep_alive"},
		{name: "missing payload", body: `null`, wantErr: "payload is required"},
		{name: "non object", body: `[]`, wantErr: "payload must be a json object"},
		{name: "missing type", body: `{"uuid":"event-uuid"}`, wantErr: "payload.type is required"},
		{name: "missing uuid", body: `{"type":"assistant"}`, wantErr: "payload.uuid is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header, err := decodeWorkerPayloadHeader(json.RawMessage(tc.body))
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || header.Type != tc.wantType {
				t.Fatalf("header = %#v, error = %v", header, err)
			}
		})
	}
}
