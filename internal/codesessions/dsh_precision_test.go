package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDSHPublicMappingsPreserveNumbers(t *testing.T) {
	input := json.RawMessage(`{"id":9007199254740993,"nested":[0.123456789012345678901,18446744073709551615]}`)
	_, payload, err := dshToolCallPublicPayload("code", toolPermissionRequest{ToolUseID: "call", ToolName: "bash"}, input, toolIdentity{Kind: "builtin"}, resolvedToolPermissionAsk, time.Now().UTC(), false)
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(payload, &event); err != nil || string(event.Input) != string(input) {
		t.Fatalf("payload=%s error=%v", payload, err)
	}
	raw := json.RawMessage(`{"id":"result","type":"tool.result","tool_use_id":"call","tool":{"kind":"builtin","name":"bash"},"content":[{"value":9007199254740993}]}`)
	batch, _, err := prepareDSHPublicEvents(db.DSHScope{CodeSessionID: "code"}, []json.RawMessage{raw})
	if err != nil || len(batch) != 1 {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	var result struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(batch[0].Events[0].Payload, &result); err != nil || string(result.Content) != `[{"value":9007199254740993}]` {
		t.Fatalf("result=%s error=%v", result.Content, err)
	}
}
