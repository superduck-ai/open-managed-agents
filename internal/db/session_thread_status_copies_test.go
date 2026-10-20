package db

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionChildStatusHasOwnAndPrimaryCopies(t *testing.T) {
	primary := SessionThread{UUID: "primary-uuid", ExternalID: "primary"}
	child := SessionThread{UUID: "child-uuid", ExternalID: "child"}
	for _, eventType := range []string{"session.thread_status_running", "session.thread_status_idle", "session.thread_status_rescheduled", "session.thread_status_terminated"} {
		for _, childOwner := range []bool{false, true} {
			source := SessionEvent{UUID: "source-uuid", ExternalID: "event-original", EventType: eventType, StatusThreadID: child.ExternalID, ProcessedAt: time.Now().UTC(), Payload: []byte(`{"id":"event-original","type":"` + eventType + `","session_thread_id":"child","agent_name":"worker","stop_reason":{"type":"end_turn"}}`)}
			if childOwner {
				source.ThreadExternalID = &child.ExternalID
			} else {
				source.ThreadExternalID = &primary.ExternalID
			}
			got, err := sessionThreadStatusCopies(source, primary, child)
			if err != nil || len(got) != 2 {
				t.Fatalf("copies=%d err=%v", len(got), err)
			}
			owners := map[string]bool{}
			for _, event := range got {
				owners[*event.ThreadExternalID] = true
			}
			if !owners[primary.ExternalID] || !owners[child.ExternalID] {
				t.Fatal("missing own or primary stream event")
			}
			var p map[string]any
			if json.Unmarshal(got[1].Payload, &p) != nil || p["type"] != eventType || p["session_thread_id"] != child.ExternalID || p["original_event_id"] != source.ExternalID || p["id"] != got[1].ExternalID || p["agent_name"] != "worker" || p["stop_reason"].(map[string]any)["type"] != "end_turn" {
				t.Fatal("mirror lost public lifecycle provenance")
			}
			if got[1].ExternalID == source.ExternalID || got[1].UUID == source.UUID || got[1].StatusThreadID != child.ExternalID || !got[1].ProcessedAt.Equal(source.ProcessedAt) {
				t.Fatal("mirror identity or timing")
			}
			again, err := sessionThreadStatusCopies(source, primary, child)
			if err != nil || again[1].ExternalID != got[1].ExternalID {
				t.Fatal("unstable replay identity")
			}
			single, err := sessionThreadStatusCopies(source, primary, primary)
			if err != nil || len(single) != 1 {
				t.Fatal("primary lifecycle duplicated")
			}
			source.Payload = []byte(`invalid`)
			if _, err := sessionThreadStatusCopies(source, primary, child); err == nil {
				t.Fatal("malformed lifecycle accepted")
			}
		}
	}
}
