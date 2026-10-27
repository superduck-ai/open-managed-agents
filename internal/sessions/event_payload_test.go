package sessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionEventResponsePreservesThreadIdentity(t *testing.T) {
	for _, test := range []struct {
		name     string
		payload  string
		owner    *string
		threadID string
		want     string
	}{
		{name: "missing owner", payload: `{"type":"agent.message"}`},
		{name: "primary history", payload: `{"type":"agent.message"}`, owner: new("sthr_primary"), want: "sthr_primary"},
		{name: "child history", payload: `{"type":"agent.message"}`, owner: new("sthr_child"), want: "sthr_child"},
		{name: "thread route", payload: `{"type":"agent.message"}`, owner: new("sthr_child"), threadID: "sthr_child", want: "sthr_child"},
		{name: "cross posted child", payload: `{"type":"agent.tool_use","session_thread_id":"sthr_child"}`, owner: new("sthr_primary"), want: "sthr_child"},
		{name: "legacy thread field", payload: `{"type":"agent.message","thread_id":"sthr_child"}`, owner: new("sthr_primary")},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := db.SessionEvent{Payload: json.RawMessage(test.payload), ThreadExternalID: test.owner, ProcessedAt: time.Now().UTC()}
			var payload struct {
				ThreadID string `json:"session_thread_id"`
			}
			if err := json.Unmarshal(sessionEventPayloadForResponse(event, test.threadID), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ThreadID != test.want {
				t.Fatalf("thread = %q, want %q", payload.ThreadID, test.want)
			}
		})
	}
}

func TestEventResponseHidesStoredThinkingContent(t *testing.T) {
	processedAt := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	stored := json.RawMessage(`{"type":"agent.thinking","id":"sevt_old","content":[{"type":"thinking","thinking":"legacy private"}],"message":{"content":[]}}`)
	var thinking map[string]any
	if err := json.Unmarshal(eventPayloadForResponse(stored, processedAt, ""), &thinking); err != nil {
		t.Fatal(err)
	}
	if _, ok := thinking["content"]; ok || thinking["message"] != nil || thinking["id"] != "sevt_old" || thinking["processed_at"] == nil {
		t.Fatalf("thinking response = %v", thinking)
	}
	message := json.RawMessage(`{"type":"agent.message","id":"sevt_text","content":[{"type":"text","text":"kept"}],"processed_at":"2026-10-19T00:00:00Z"}`)
	if got := eventPayloadForResponse(message, processedAt, ""); string(got) != string(message) {
		t.Fatalf("agent.message response changed: %s", got)
	}
}

func TestThreadMessageResponseUsesStoredOwnerAndPreservesPeer(t *testing.T) {
	for _, eventType := range []string{"agent.thread_message_sent", "agent.thread_message_received"} {
		for _, owner := range []string{"primary", "child"} {
			payload := json.RawMessage(`{"id":"event","type":"` + eventType + `","session_thread_id":"wrong","to_session_thread_id":"peer","from_session_thread_id":"peer","original_event_id":"original","content":[{"type":"text","text":"literal"}]}`)
			source := db.SessionEvent{Payload: payload, ThreadExternalID: &owner}
			var got map[string]any
			if err := json.Unmarshal(sessionEventPayloadForResponse(source, ""), &got); err != nil {
				t.Fatal(err)
			}
			if got["session_thread_id"] != owner || got["to_session_thread_id"] != "peer" || got["from_session_thread_id"] != "peer" || got["id"] != "event" || got["original_event_id"] != "original" {
				t.Fatalf("owner or peer changed incorrectly: %#v", got)
			}
			if string(source.Payload) != string(payload) {
				t.Fatal("stored event was mutated")
			}
		}
	}
}
