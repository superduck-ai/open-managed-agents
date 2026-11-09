package messages

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestThinkingStatusStartFailures(t *testing.T) {
	t.Run("duplicate block start does not publish twice", func(t *testing.T) {
		count := 0
		o := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}, publishThinking: func(string, int) bool { count++; return true }}
		o.observeFrame([]byte(`{"type":"message_start","message":{"id":"msg_test"}}`))
		for range 2 {
			o.observeFrame([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`))
		}
		o.finish()
		if count != 1 || o.result.ErrorType != "invalid_response" {
			t.Fatalf("starts=%d error=%s", count, o.result.ErrorType)
		}
	})
	t.Run("failed start publication falls back to same status ID", func(t *testing.T) {
		o := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}, publishThinking: func(string, int) bool { return false }}
		for _, frame := range []string{`{"type":"message_start","message":{"id":"msg_test"}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"private"}}`, `{"type":"message_stop"}`} {
			o.observeFrame([]byte(frame))
		}
		if len(o.result.Messages) != 1 || o.result.Messages[0].ID != maevents.StableAssistantEventID("cse_test", "msg_test", 0, "agent.thinking") || len(o.result.Messages[0].Content) != 0 {
			t.Fatalf("fallback=%+v", o.result.Messages)
		}
	})
}

func TestThinkingStatusPublishesBeforeAnswerWithoutBody(t *testing.T) {
	for _, kind := range []string{"thinking", "redacted_thinking"} {
		t.Run(kind, func(t *testing.T) {
			var ids []string
			o := &responseObservation{streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_test"}}
			o.publishThinking = func(messageID string, index int) bool {
				ids = append(ids, maevents.StableAssistantEventID("cse_test", messageID, index, "agent.thinking"))
				return true
			}
			o.observeFrame([]byte(`{"type":"message_start","message":{"id":"msg_test"}}`))
			start, _ := json.Marshal(responseStreamEvent{Type: "content_block_start", Index: 0, ContentBlock: responseContentBlock{Type: kind}})
			o.observeFrame(start)
			if len(ids) != 1 || o.complete || len(o.result.Messages) != 0 {
				t.Fatalf("thinking not published at block start: %v", ids)
			}
			for _, frame := range []string{`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private reasoning"}}`, `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"answer"}}`, `{"type":"message_stop"}`} {
				o.observeFrame([]byte(frame))
			}
			o.finish()
			if o.result.ErrorType != "" || len(o.result.Messages) != 2 || o.result.Messages[0].Type != "agent.thinking" || o.result.Messages[0].ID != ids[0] || o.result.Messages[1].Type != "agent.message" || !slices.Equal(o.result.EventIDs, []string{ids[0], o.result.Messages[1].ID}) {
				t.Fatalf("events=%+v result=%+v", ids, o.result)
			}
			encoded, _ := json.Marshal(o.result.Messages)
			if strings.Contains(string(encoded), "private reasoning") {
				t.Fatalf("public result exposed thinking: %s", encoded)
			}
		})
	}
}
