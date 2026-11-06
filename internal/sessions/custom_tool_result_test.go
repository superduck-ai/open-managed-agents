package sessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func TestCustomToolResultReferenceNormalization(t *testing.T) {
	payload := map[string]any{"type": "user.custom_tool_result", "custom_tool_use_id": "public-call", "content": []any{map[string]any{"type": "text", "text": "done"}}}
	if err := validatePublicInputEvent("user.custom_tool_result", payload); err != nil {
		t.Fatal(err)
	}
	if id := sessionToolUseID(payload); id != "public-call" {
		t.Fatalf("normalized=%q", id)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, normalized, err := eventpayload.Summarize(raw, "user.custom_tool_result")
	if err != nil || normalized == nil || *normalized != "public-call" {
		t.Fatalf("eventpayload normalized=%v %v", normalized, err)
	}
}
