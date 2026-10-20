package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestMCPResultMapping(t *testing.T) {
	for _, isError := range []bool{true, false} {
		for _, threadID := range []string{"", "sthr_child"} {
			raw := mustRawJSON(t, map[string]any{"type": "user", "uuid": "result", "message": map[string]any{"content": []any{
				map[string]any{"type": "tool_result", "tool_name": "mcp__peer__microsoft_docs_fetch", "tool_use_id": "call_same", "is_error": isError, "content": []any{map[string]any{"type": "text", "text": "agentId: documentation\n<usage>example</usage>"}}},
			}}})
			var payloads []json.RawMessage
			var err error
			if threadID == "" {
				payloads, _, err = publicPayloadsFromWorkerEvent("cse_test", db.CodeSessionEvent{EventType: "user", CreatedAt: time.Now()}, raw)
			} else {
				payloads, err = publicPayloadsFromInternalSubagentEvent("cse_test", db.CodeSessionInternalEvent{Payload: raw, CreatedAt: time.Now()}, threadID)
			}
			if err != nil {
				t.Fatal(err)
			}
			objects := decodePublicPayloads(t, payloads)
			if len(objects) != 1 {
				t.Fatalf("payloads = %s", payloads)
			}
			result := objects[0]
			if result["type"] != "agent.mcp_tool_result" || result["mcp_tool_use_id"] != maevents.ToolUseEventID("cse_test", "call_same", threadID) || result["is_error"] != isError {
				t.Fatalf("MCP result = %#v", result)
			}
			for _, field := range []string{"tool_use_id", "tool_name", "from_session_thread_id", "message"} {
				if _, exists := result[field]; exists {
					t.Fatalf("MCP result leaked %s: %#v", field, result)
				}
			}
			content := result["content"].([]any)
			if content[0].(map[string]any)["text"] != "agentId: documentation\n<usage>example</usage>" {
				t.Fatalf("MCP content changed: %#v", content)
			}
		}
	}
}

func TestBuiltinResultsKeepOrdinaryReference(t *testing.T) {
	raw := json.RawMessage(`{"type":"user","message":{"content":[{"type":"tool_result","tool_name":"Bash","tool_use_id":"call_same","is_error":false,"content":"done"}]}}`)
	payloads, _, err := publicPayloadsFromWorkerEvent("cse_test", db.CodeSessionEvent{EventType: "user"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	result := decodePublicPayloads(t, payloads)[0]
	if result["type"] != "agent.tool_result" || result["tool_use_id"] != maevents.ToolUseEventID("cse_test", "call_same", "") {
		t.Fatalf("builtin result = %#v", result)
	}
	if _, exists := result["mcp_tool_use_id"]; exists {
		t.Fatalf("builtin result acquired MCP reference: %#v", result)
	}
}
