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
