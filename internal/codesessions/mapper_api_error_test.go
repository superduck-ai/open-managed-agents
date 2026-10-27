package codesessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWorkerAPIErrorAssistantIsPrivate(t *testing.T) {
	raw := json.RawMessage(`{"type":"assistant","uuid":"api-error","is_api_error_message":true,"message":{"id":"synthetic-error","content":[{"type":"text","text":"private provider error"}]}}`)
	payloads, ok, err := publicPayloadsFromWorkerEvent("cse_test", db.CodeSessionEvent{EventType: "assistant"}, raw)
	if err != nil || ok || len(payloads) != 0 {
		t.Fatalf("API error published as main reply: %s %v", payloads, err)
	}
	payloads, err = publicPayloadsFromInternalSubagentEvent("cse_test", db.CodeSessionInternalEvent{Payload: raw}, "sthr_child")
	if err != nil || len(payloads) != 0 {
		t.Fatalf("API error published as child reply: %s %v", payloads, err)
	}
}
