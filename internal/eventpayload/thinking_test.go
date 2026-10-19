package eventpayload

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWithoutThinkingContentRejectsInvalidThinkingPayload(t *testing.T) {
	if _, err := withoutThinkingContent([]db.SessionEvent{{EventType: "agent.thinking", Payload: json.RawMessage(`[`)}}); err == nil {
		t.Fatal("withoutThinkingContent() error = nil")
	}
}

func TestWithoutThinkingContentKeepsOnlyProgressFields(t *testing.T) {
	original := []db.SessionEvent{
		{EventType: "agent.thinking", Payload: json.RawMessage(`{"type":"agent.thinking","id":"sevt_1","content":[{"type":"thinking","thinking":"private"}],"message":{"content":[]},"model_request_start_id":"sevt_r","session_thread_id":"sthr_1"}`)},
		{EventType: "agent.message", Payload: json.RawMessage(`{"type":"agent.message","id":"sevt_2","content":[{"type":"text","text":"visible"}]}`)},
		{EventType: "agent.thinking", Payload: json.RawMessage(`{"type":"agent.thinking","id":"sevt_3"}`)},
	}
	stripped, err := withoutThinkingContent(original)
	if err != nil {
		t.Fatal(err)
	}
	var thinking map[string]any
	if err := json.Unmarshal(stripped[0].Payload, &thinking); err != nil {
		t.Fatal(err)
	}
	if _, ok := thinking["content"]; ok || thinking["message"] != nil || thinking["id"] != "sevt_1" || thinking["model_request_start_id"] != "sevt_r" || thinking["session_thread_id"] != "sthr_1" {
		t.Fatalf("stripped thinking = %s", stripped[0].Payload)
	}
	if string(stripped[1].Payload) != string(original[1].Payload) || string(stripped[2].Payload) != string(original[2].Payload) {
		t.Fatalf("non-content events changed: %s %s", stripped[1].Payload, stripped[2].Payload)
	}
	if string(original[0].Payload) == string(stripped[0].Payload) {
		t.Fatal("caller events were not copied before stripping")
	}
}
