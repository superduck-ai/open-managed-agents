package db

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

func TestWebSearchResultIDSkipsFailuresAndExistingPrices(t *testing.T) {
	event := SessionEvent{EventType: "agent.tool_result", WorkspaceUUID: "ws", SessionExternalID: "ses", Payload: json.RawMessage(`{"tool_use_id":"call","is_error":false}`)}
	id, ok := webSearchResultID(event)
	if !ok || id != "call" {
		t.Fatalf("successful result id = %q, ok %v", id, ok)
	}
	event.Payload = json.RawMessage(`{"tool_use_id":"call","is_error":true}`)
	if _, ok := webSearchResultID(event); ok {
		t.Fatal("failed result should not record a price")
	}
	event.Payload = json.RawMessage(`{"tool_use_id":"call","tool_price":{"cny":"5","per":1000,"exchange":"7.2"}}`)
	if _, ok := webSearchResultID(event); ok {
		t.Fatal("existing price should stay")
	}
}

func TestMergeToolPriceRecordsCurrentRate(t *testing.T) {
	raw, err := mergeToolPrice(json.RawMessage(`{"tool_use_id":"call","is_error":false}`))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		ToolUseID string              `json:"tool_use_id"`
		ToolPrice billing.SearchPrice `json:"tool_price"`
	}
	if json.Unmarshal(raw, &body) != nil || body.ToolUseID != "call" || body.ToolPrice != billing.CurrentSearchPrice() {
		t.Fatalf("stamped payload = %s", raw)
	}
}
