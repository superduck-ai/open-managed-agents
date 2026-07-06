package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPublicPayloadFromWorkerEventPassesThroughCanonicalOutputs(t *testing.T) {
	outputs := []string{
		"agent.message",
		"agent.thinking",
		"agent.tool_use",
		"agent.tool_result",
		"agent.mcp_tool_use",
		"agent.mcp_tool_result",
		"agent.custom_tool_use",
		"agent.thread_context_compacted",
		"agent.thread_message_received",
		"agent.thread_message_sent",
		"session.status_running",
		"session.status_idle",
		"session.status_rescheduled",
		"session.status_terminated",
		"session.deleted",
		"session.updated",
		"session.error",
		"session.thread_created",
		"session.thread_status_running",
		"session.thread_status_idle",
		"session.thread_status_rescheduled",
		"session.thread_status_terminated",
		"span.model_request_start",
		"span.model_request_end",
		"span.outcome_evaluation_start",
		"span.outcome_evaluation_ongoing",
		"span.outcome_evaluation_end",
		"system.message",
	}
	for _, eventType := range outputs {
		t.Run(eventType, func(t *testing.T) {
			createdAt := time.Date(2026, 6, 16, 1, 10, 0, 0, time.UTC)
			payload, ok, err := publicPayloadFromWorkerEvent("csev_test", db.CodeSessionEvent{
				ExternalID:     "csev_" + eventType,
				EventType:      eventType,
				IdempotencyKey: "idem_" + eventType,
				CreatedAt:      createdAt,
			}, map[string]any{
				"type":       eventType,
				"uuid":       "uuid_" + eventType,
				"created_at": "2026-06-16T01:10:00Z",
			})
			if err != nil {
				t.Fatalf("publicPayloadFromWorkerEvent returned error: %v", err)
			}
			if !ok {
				t.Fatal("publicPayloadFromWorkerEvent ok = false, want true")
			}
			var object map[string]any
			if err := json.Unmarshal(payload, &object); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if object["type"] != eventType {
				t.Fatalf("payload type = %q, want %q; payload=%s", object["type"], eventType, payload)
			}
			if object["id"] == "" || object["created_at"] == "" || object["processed_at"] == "" || object["session_id"] != nil {
				t.Fatalf("canonical payload missing normalized public fields: %s", payload)
			}
		})
	}
}

func TestPublicPayloadFromWorkerEventRejectsClientOnlyAndHiddenEvents(t *testing.T) {
	for _, eventType := range []string{
		"user.tool_result",
		"user.tool_confirmation",
		"user.custom_tool_result",
		"user.define_outcome",
		"user.interrupt",
		"env_manager_log",
		"control_request",
	} {
		t.Run(eventType, func(t *testing.T) {
			_, ok, err := publicPayloadFromWorkerEvent("csev_test", db.CodeSessionEvent{
				ExternalID:     "csev_" + eventType,
				EventType:      eventType,
				IdempotencyKey: "idem_" + eventType,
				CreatedAt:      time.Date(2026, 6, 16, 1, 10, 0, 0, time.UTC),
			}, map[string]any{"type": eventType, "uuid": "uuid_" + eventType})
			if err != nil {
				t.Fatalf("publicPayloadFromWorkerEvent returned error: %v", err)
			}
			if ok {
				t.Fatal("publicPayloadFromWorkerEvent ok = true, want false")
			}
		})
	}
}

func TestPublicPayloadFromWorkerEventPassesThroughStreamPreview(t *testing.T) {
	for _, eventType := range []string{"event_start", "event_delta"} {
		t.Run(eventType, func(t *testing.T) {
			payload, ok, err := publicPayloadFromWorkerEvent("csev_test", db.CodeSessionEvent{
				ExternalID:     "csev_" + eventType,
				EventType:      eventType,
				IdempotencyKey: "idem_" + eventType,
				CreatedAt:      time.Date(2026, 6, 16, 1, 10, 0, 0, time.UTC),
			}, map[string]any{"type": eventType, "uuid": "uuid_" + eventType, "delta": map[string]any{"text": "preview"}})
			if err != nil {
				t.Fatalf("publicPayloadFromWorkerEvent returned error: %v", err)
			}
			if !ok {
				t.Fatal("publicPayloadFromWorkerEvent ok = false, want true")
			}
			var object map[string]any
			if err := json.Unmarshal(payload, &object); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if object["type"] != eventType || object["id"] == "" || object["created_at"] == "" {
				t.Fatalf("stream preview payload missing normalized fields: %s", payload)
			}
		})
	}
}

func TestPublicPayloadsFromWorkerEventMapsClaudeAssistantBlocks(t *testing.T) {
	payloads, ok, err := publicPayloadsFromWorkerEvent("csev_test", db.CodeSessionEvent{
		ExternalID:     "csev_assistant_blocks",
		EventType:      "assistant",
		IdempotencyKey: "assistant_blocks",
		CreatedAt:      time.Date(2026, 6, 16, 1, 10, 0, 0, time.UTC),
	}, map[string]any{
		"type": "assistant",
		"uuid": "assistant-blocks",
		"message": map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "thinking", "thinking": "plan"},
				map[string]any{"type": "text", "text": "starting"},
				map[string]any{
					"type":  "tool_use",
					"id":    "tool_translate",
					"name":  "Agent",
					"input": map[string]any{"description": "Translate to Chinese"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("publicPayloadsFromWorkerEvent returned error: %v", err)
	}
	if !ok {
		t.Fatal("publicPayloadsFromWorkerEvent ok = false, want true")
	}
	objects := decodePublicPayloads(t, payloads)
	if got, want := len(objects), 3; got != want {
		t.Fatalf("payload count = %d, want %d: %#v", got, want, objects)
	}
	wantTypes := []string{"agent.thinking", "agent.message", "agent.tool_use"}
	for index, wantType := range wantTypes {
		if objects[index]["type"] != wantType {
			t.Fatalf("payload[%d] type = %q, want %q; payload=%#v", index, objects[index]["type"], wantType, objects[index])
		}
		if objects[index]["id"] == "" || objects[index]["processed_at"] == "" {
			t.Fatalf("payload[%d] missing normalized fields: %#v", index, objects[index])
		}
	}
	toolPayload := objects[2]
	if toolPayload["tool_use_id"] != "tool_translate" || toolPayload["name"] != "Agent" || toolPayload["tool_name"] != "Agent" {
		t.Fatalf("tool payload missing tool fields: %#v", toolPayload)
	}
	input, ok := toolPayload["input"].(map[string]any)
	if !ok || input["description"] != "Translate to Chinese" {
		t.Fatalf("tool payload input = %#v", toolPayload["input"])
	}
}

func TestPublicPayloadsFromInternalSubagentEventMarksOwnerThread(t *testing.T) {
	payloads, err := publicPayloadsFromInternalSubagentEvent("csev_test", db.CodeSessionInternalEvent{
		ExternalID:     "csie_subagent_assistant",
		EventType:      "assistant",
		IdempotencyKey: "subagent_assistant",
		AgentID:        ptrString("task_123"),
		CreatedAt:      time.Date(2026, 6, 16, 1, 10, 0, 0, time.UTC),
		Payload: mustRawJSON(t, map[string]any{
			"type":    "assistant",
			"uuid":    "assistant-subagent",
			"agentId": "task_123",
			"message": map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": "こんにちは、世界"},
				},
			},
		}),
	}, "sthr_child123")
	if err != nil {
		t.Fatalf("publicPayloadsFromInternalSubagentEvent returned error: %v", err)
	}
	objects := decodePublicPayloads(t, payloads)
	if len(objects) != 1 {
		t.Fatalf("payload count = %d, want 1: %#v", len(objects), objects)
	}
	if objects[0]["type"] != "agent.message" || objects[0]["_owner_session_thread_id"] != "sthr_child123" {
		t.Fatalf("subagent payload missing child owner: %#v", objects[0])
	}
	if objects[0]["agent_id"] != "task_123" {
		t.Fatalf("subagent payload missing agent_id: %#v", objects[0])
	}
	if _, ok := objects[0]["agentId"]; ok {
		t.Fatalf("subagent payload leaked agentId alias: %#v", objects[0])
	}
}

func TestPublicPayloadsFromWorkerEventMapsClaudeUserToolResults(t *testing.T) {
	plainPayloads, ok, err := publicPayloadsFromWorkerEvent("csev_test", db.CodeSessionEvent{
		ExternalID:     "csev_user_echo",
		EventType:      "user",
		IdempotencyKey: "user_echo",
		CreatedAt:      time.Date(2026, 6, 16, 1, 10, 0, 0, time.UTC),
	}, map[string]any{
		"type":    "user",
		"uuid":    "user-echo",
		"message": map[string]any{"role": "user", "content": "duplicate user prompt"},
	})
	if err != nil {
		t.Fatalf("plain user mapping returned error: %v", err)
	}
	if ok || len(plainPayloads) != 0 {
		t.Fatalf("plain Claude user echo should not publish public events: ok=%v payloads=%#v", ok, plainPayloads)
	}

	payloads, ok, err := publicPayloadsFromWorkerEvent("csev_test", db.CodeSessionEvent{
		ExternalID:     "csev_user_tool_result",
		EventType:      "user",
		IdempotencyKey: "user_tool_result",
		CreatedAt:      time.Date(2026, 6, 16, 1, 10, 5, 0, time.UTC),
	}, map[string]any{
		"type": "user",
		"uuid": "user-tool-result",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "tool_translate",
					"content": []any{
						map[string]any{"type": "text", "text": "你好，世界"},
						map[string]any{"type": "text", "text": "agentId: agent_123\n<usage>total_tokens: 12</usage>"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("tool_result user mapping returned error: %v", err)
	}
	if !ok {
		t.Fatal("tool_result user mapping ok = false, want true")
	}
	objects := decodePublicPayloads(t, payloads)
	if len(objects) != 1 {
		t.Fatalf("tool_result payload count = %d, want 1: %#v", len(objects), objects)
	}
	expectedThreadID := claudeTaskThreadIDFromKey("csev_test", "tool_translate")
	if objects[0]["type"] != "agent.thread_message_received" || objects[0]["from_session_thread_id"] != expectedThreadID {
		t.Fatalf("tool_result mapped payload = %#v", objects[0])
	}
	content, ok := objects[0]["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("tool_result content = %#v", objects[0]["content"])
	}
	firstContent, _ := content[0].(map[string]any)
	if firstContent["text"] != "你好，世界" {
		t.Fatalf("tool_result visible content = %#v", content)
	}

	toolPayloads, ok, err := publicPayloadsFromWorkerEvent("csev_test", db.CodeSessionEvent{
		ExternalID:     "csev_user_plain_tool_result",
		EventType:      "user",
		IdempotencyKey: "user_plain_tool_result",
		CreatedAt:      time.Date(2026, 6, 16, 1, 10, 6, 0, time.UTC),
	}, map[string]any{
		"type": "user",
		"uuid": "user-plain-tool-result",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "tool_bash",
					"is_error":    false,
					"content": []any{
						map[string]any{"type": "text", "text": "extracted 3 files"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("plain tool_result user mapping returned error: %v", err)
	}
	if !ok {
		t.Fatal("plain tool_result user mapping ok = false, want true")
	}
	toolObjects := decodePublicPayloads(t, toolPayloads)
	if len(toolObjects) != 1 {
		t.Fatalf("plain tool_result payload count = %d, want 1: %#v", len(toolObjects), toolObjects)
	}
	if toolObjects[0]["type"] != "agent.tool_result" || toolObjects[0]["tool_use_id"] != "tool_bash" {
		t.Fatalf("plain tool_result mapped payload = %#v", toolObjects[0])
	}
	if _, ok := toolObjects[0]["from_session_thread_id"]; ok {
		t.Fatalf("plain tool_result should not carry subagent thread id: %#v", toolObjects[0])
	}
}

func TestPublicPayloadsFromWorkerEventMapsClaudeResultToModelSpansAndIdle(t *testing.T) {
	createdAt := time.Date(2026, 6, 16, 1, 11, 0, 0, time.UTC)
	payloads, ok, err := publicPayloadsFromWorkerEvent("csev_test", db.CodeSessionEvent{
		ExternalID:     "csev_result",
		EventType:      "result",
		IdempotencyKey: "result_event",
		CreatedAt:      createdAt,
	}, map[string]any{
		"type":            "result",
		"uuid":            "result-uuid",
		"created_at":      "2026-06-16T01:11:00Z",
		"duration_ms":     float64(9000),
		"duration_api_ms": float64(12000),
		"result":          "Done.",
		"usage": map[string]any{
			"input_tokens":  float64(1),
			"output_tokens": float64(866),
		},
		"modelUsage": map[string]any{
			"claude-sonnet-4-6": map[string]any{
				"inputTokens":  float64(1),
				"outputTokens": float64(866),
			},
		},
	})
	if err != nil {
		t.Fatalf("result mapping returned error: %v", err)
	}
	if !ok {
		t.Fatal("result mapping ok = false, want true")
	}
	objects := decodePublicPayloads(t, payloads)
	wantTypes := []string{"span.model_request_start", "span.model_request_end", "session.status_idle"}
	if len(objects) != len(wantTypes) {
		t.Fatalf("result payload count = %d, want %d: %#v", len(objects), len(wantTypes), objects)
	}
	for index, wantType := range wantTypes {
		if objects[index]["type"] != wantType {
			t.Fatalf("result payload[%d] type = %q, want %q: %#v", index, objects[index]["type"], wantType, objects[index])
		}
	}
	startedAt, err := time.Parse(time.RFC3339Nano, objects[0]["created_at"].(string))
	if err != nil {
		t.Fatalf("parse model start created_at: %v", err)
	}
	endedAt, err := time.Parse(time.RFC3339Nano, objects[1]["created_at"].(string))
	if err != nil {
		t.Fatalf("parse model end created_at: %v", err)
	}
	if !startedAt.Before(endedAt) {
		t.Fatalf("model start should be before end: start=%s end=%s", startedAt, endedAt)
	}
	if objects[1]["model"] != "claude-sonnet-4-6" {
		t.Fatalf("model_request_end model = %#v", objects[1])
	}
	if _, ok := objects[1]["model_usage"].(map[string]any); !ok {
		t.Fatalf("model_request_end missing model_usage: %#v", objects[1])
	}
	if objects[1]["model_request_start_id"] != objects[0]["id"] {
		t.Fatalf("model_request_end model_request_start_id = %#v, want start id %#v", objects[1]["model_request_start_id"], objects[0]["id"])
	}
	if objects[2]["result"] != "Done." {
		t.Fatalf("status idle should preserve result payload: %#v", objects[2])
	}
}

func TestPublicPayloadsFromWorkerEventMapsClaudeTaskLifecycle(t *testing.T) {
	startPayloads, ok, err := publicPayloadsFromWorkerEvent("csev_test", db.CodeSessionEvent{
		ExternalID:     "csev_task_started",
		EventType:      "system",
		IdempotencyKey: "task_started",
		CreatedAt:      time.Date(2026, 6, 16, 1, 10, 0, 0, time.UTC),
	}, map[string]any{
		"type":        "system",
		"uuid":        "system-task-started",
		"subtype":     "task_started",
		"task_id":     "abc123",
		"tool_use_id": "tool_translate",
		"description": "Translate to Japanese",
		"prompt":      "Translate hello, world to Japanese.",
	})
	if err != nil {
		t.Fatalf("task_started mapping returned error: %v", err)
	}
	if !ok {
		t.Fatal("task_started mapping ok = false, want true")
	}
	startObjects := decodePublicPayloads(t, startPayloads)
	wantStartTypes := []string{"session.thread_created", "session.thread_status_running", "agent.thread_message_sent"}
	if len(startObjects) != len(wantStartTypes) {
		t.Fatalf("task_started payload count = %d, want %d: %#v", len(startObjects), len(wantStartTypes), startObjects)
	}
	for index, wantType := range wantStartTypes {
		if startObjects[index]["type"] != wantType {
			t.Fatalf("task_started payload[%d] type = %q, want %q", index, startObjects[index]["type"], wantType)
		}
	}
	expectedThreadID := claudeTaskThreadIDFromKey("csev_test", "tool_translate")
	if startObjects[0]["session_thread_id"] != expectedThreadID || startObjects[0]["agent_name"] != "Translate to Japanese" {
		t.Fatalf("thread_created payload = %#v", startObjects[0])
	}
	if startObjects[2]["to_session_thread_id"] != expectedThreadID || startObjects[2]["to_agent_name"] != "Translate to Japanese" {
		t.Fatalf("thread_message_sent payload = %#v", startObjects[2])
	}

	donePayloads, ok, err := publicPayloadsFromWorkerEvent("csev_test", db.CodeSessionEvent{
		ExternalID:     "csev_task_done",
		EventType:      "system",
		IdempotencyKey: "task_done",
		CreatedAt:      time.Date(2026, 6, 16, 1, 10, 5, 0, time.UTC),
	}, map[string]any{
		"type":        "system",
		"uuid":        "system-task-done",
		"subtype":     "task_notification",
		"task_id":     "abc123",
		"tool_use_id": "tool_translate",
		"status":      "completed",
		"summary":     "Translate to Japanese",
		"usage":       map[string]any{"duration_ms": float64(5000)},
	})
	if err != nil {
		t.Fatalf("task_notification mapping returned error: %v", err)
	}
	if !ok {
		t.Fatal("task_notification mapping ok = false, want true")
	}
	doneObjects := decodePublicPayloads(t, donePayloads)
	if len(doneObjects) != 1 || doneObjects[0]["type"] != "session.thread_status_idle" {
		t.Fatalf("task_notification payloads = %#v", doneObjects)
	}
	if doneObjects[0]["session_thread_id"] != expectedThreadID {
		t.Fatalf("thread_status_idle payload = %#v", doneObjects[0])
	}
	stopReason, ok := doneObjects[0]["stop_reason"].(map[string]any)
	if !ok || stopReason["type"] != "completed" || stopReason["detail"] != "Translate to Japanese" {
		t.Fatalf("stop_reason = %#v", doneObjects[0]["stop_reason"])
	}
}

func decodePublicPayloads(t *testing.T, payloads []json.RawMessage) []map[string]any {
	t.Helper()
	objects := make([]map[string]any, 0, len(payloads))
	for _, payload := range payloads {
		var object map[string]any
		if err := json.Unmarshal(payload, &object); err != nil {
			t.Fatalf("decode payload %s: %v", payload, err)
		}
		objects = append(objects, object)
	}
	return objects
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return raw
}

func ptrString(value string) *string {
	return &value
}
