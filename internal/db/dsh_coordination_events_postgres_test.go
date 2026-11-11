//go:build integration

package db

import (
	"encoding/json"
	"testing"
	"time"
	"uuid"
)

func TestDSHCoordinationPublicIdentityPostgres(t *testing.T) {
	f, primary := newDSRTurnBoundaryFixture(t)
	parent := "root"
	if _, err := f.store.AppendDSHTranscript(f.ctx, f.scope, "child", &parent, []DSHTranscriptRecord{{Offset: 0, Payload: []byte(`{"version":4,"id":"child"}`)}}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"session.thread_created", "agent.thread_message_sent", "agent.thread_message_received"} {
		payload := map[string]any{"id": kind, "type": kind, "content": []map[string]string{{"type": "text", "text": "task"}}}
		item := DSHPublicEvent{ID: kind, DSHSessionID: "child"}
		switch kind {
		case "session.thread_created":
			payload["parent_thread_id"] = f.scope.CodeSessionID
			payload["session_thread_id"] = "child"
			item.Thread = &SessionThread{ExternalID: "child", AgentSnapshot: []byte(`{"name":"Reader","type":"agent"}`), Status: "idle", Usage: []byte(`{}`), Stats: []byte(`{}`), CreatedAt: time.Now().UTC()}
		case "agent.thread_message_sent":
			payload["to_session_thread_id"] = "child"
		case "agent.thread_message_received":
			payload["from_session_thread_id"] = "child"
		}
		raw, _ := json.Marshal(payload)
		item.Payload = raw
		item.Events = []SessionEvent{{UUID: uuid.NewV4().String(), ExternalID: kind, EventType: kind, Payload: raw, CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}}
		result, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
		if err != nil || result.Accepted != 1 {
			t.Fatalf("%s: %+v %v", kind, result, err)
		}
		mirrors := 0
		var actual map[string]any
		for _, event := range result.Changes.Events {
			if event.ThreadExternalID != nil && *event.ThreadExternalID == "child" {
				var copy map[string]any
				if err := json.Unmarshal(event.Payload, &copy); err != nil {
					t.Fatal(err)
				}
				if copy["original_event_id"] == kind {
					mirrors++
				}
			}
			if event.ExternalID == kind {
				if err := json.Unmarshal(event.Payload, &actual); err != nil {
					t.Fatal(err)
				}
			}
		}
		switch kind {
		case "session.thread_created":
			if actual["parent_thread_id"] != primary.ExternalID || actual["agent_name"] != "Reader" || actual["agent"] == nil {
				t.Fatal(actual)
			}
		case "agent.thread_message_sent":
			if actual["to_agent_name"] != "Reader" {
				t.Fatal(actual)
			}
		case "agent.thread_message_received":
			if actual["from_agent_name"] != "Reader" {
				t.Fatal(actual)
			}
		}
		if kind != "session.thread_created" && mirrors != 1 {
			t.Fatalf("missing child coordination mirror: %d", mirrors)
		}
		replay, err := f.store.PublishDSHEvents(f.ctx, f.scope, []DSHPublicEvent{item})
		if err != nil || replay.Duplicates != 1 {
			t.Fatalf("replay %s: %+v %v", kind, replay, err)
		}
	}
}
