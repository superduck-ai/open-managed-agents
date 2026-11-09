package sessions

import "testing"

func TestChildThreadMessageCopyReversesDirectionAndKeepsIdentity(t *testing.T) {
	for _, tc := range []struct{ kind, want, field string }{{"agent.thread_message_sent", "agent.thread_message_received", "from_session_thread_id"}, {"agent.thread_message_received", "agent.thread_message_sent", "to_session_thread_id"}} {
		t.Run(tc.kind, func(t *testing.T) {
			input := map[string]any{"id": "source", "type": tc.kind, "session_thread_id": "child", "to_session_thread_id": "child", "from_session_thread_id": "child", "content": []any{map[string]any{"type": "text", "text": "literal"}}, "processed_at": "2026-10-16T13:13:43Z"}
			got := childThreadMessageCopy("code", tc.kind, "source", input, "child", "primary", "lead")
			if got.OwnerThreadID == nil || *got.OwnerThreadID != "child" || got.Payload["type"] != tc.want || got.Payload[tc.field] != "primary" || got.Payload["original_event_id"] != "source" {
				t.Fatalf("invalid projection %#v", got)
			}
			if got.EventID == "source" || got.Payload["id"] != got.EventID {
				t.Fatal("counterpart needs its own stable event ID")
			}
			again := childThreadMessageCopy("code", tc.kind, "source", input, "child", "primary", "lead")
			if again.EventID != got.EventID {
				t.Fatal("replay changed event identity")
			}
			if got.Payload["processed_at"] != input["processed_at"] || got.Payload["content"] == nil || input["type"] != tc.kind || input["id"] != "source" {
				t.Fatal("source content or identity changed")
			}
			other := childThreadMessageCopy("code", tc.kind, "source", input, "other-child", "primary", "lead")
			if other.EventID == got.EventID {
				t.Fatal("different thread reused projection identity")
			}
		})
	}
}

func TestThreadMessageCopiesKeepBothOwnersAndPeers(t *testing.T) {
	for _, kind := range []string{"agent.thread_message_sent", "agent.thread_message_received"} {
		input := map[string]any{"id": "source", "uuid": "transport", "type": kind, "session_thread_id": "child", "agent_name": "worker", "content": []any{map[string]any{"type": "text", "text": "literal"}}}
		peerField := "to_session_thread_id"
		if kind == "agent.thread_message_received" {
			peerField = "from_session_thread_id"
		}
		input[peerField] = "child"
		primary := primaryThreadMessageCopy("source", input, "primary")
		child := childThreadMessageCopy("code", kind, "source", input, "child", "primary", "lead")
		if primary.Payload["session_thread_id"] != "primary" || primary.Payload[peerField] != "child" || primary.Payload["agent_name"] != nil {
			t.Fatalf("invalid primary projection: %#v", primary)
		}
		if primary.EventID != "source" || primary.EventID == child.EventID || primary.Payload["uuid"] != child.Payload["uuid"] {
			t.Fatal("message perspectives lost stable independent identities")
		}
		if input["session_thread_id"] != "child" || input["agent_name"] != "worker" {
			t.Fatal("input was mutated")
		}
	}
}
