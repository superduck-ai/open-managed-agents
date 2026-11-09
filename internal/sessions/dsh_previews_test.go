package sessions

import (
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestDSHPreviewsRejectUnresolvablePrimaryBeforeFanout(t *testing.T) {
	failure := errors.New("unavailable")
	events, err := dshPreviewEvents(db.DSHScope{}, []json.RawMessage{json.RawMessage(`{"type":"event_delta","event_id":"m"}`)}, func() (string, error) { return "", failure })
	if !errors.Is(err, failure) || events != nil {
		t.Fatalf("events=%v err=%v", events, err)
	}
}
func TestDSHPreviewsResolvePrimaryOnlyOnceAndPreserveDeltas(t *testing.T) {
	payloads := []json.RawMessage{
		json.RawMessage(`{"type":"event_start","event":{"id":"m"}}`),
		json.RawMessage(`{"type":"event_delta","event_id":"m","delta":{"content":{"text":"hello"}}}`),
		json.RawMessage(`{"type":"event_delta","event_id":"m","delta":{"content":{"text":" world"}}}`),
		json.RawMessage(`{"type":"event_delta","session_thread_id":"child","event_id":"c"}`),
	}
	calls := 0
	events, err := dshPreviewEvents(db.DSHScope{SessionID: "session", WorkspaceUUID: "workspace"}, payloads, func() (string, error) { calls++; return "primary", nil })
	if err != nil || calls != 1 || len(events) != 4 {
		t.Fatalf("calls=%d events=%v err=%v", calls, events, err)
	}
	for i, event := range events {
		thread := "primary"
		if i == 3 {
			thread = "child"
		}
		if *event.ThreadExternalID != thread || string(event.Payload) != string(payloads[i]) || event.SessionExternalID != "session" || event.WorkspaceUUID != "workspace" {
			t.Fatalf("event=%+v", event)
		}
	}
	if events[0].ExternalID != "m" || events[3].ExternalID != "c" {
		t.Fatalf("events=%+v", events)
	}
}
func TestDSHChildPreviewsDoNotResolvePrimary(t *testing.T) {
	_, err := dshPreviewEvents(db.DSHScope{}, []json.RawMessage{json.RawMessage(`{"type":"event_delta","session_thread_id":"child","event_id":"c"}`)}, func() (string, error) { t.Fatal("unexpected primary lookup"); return "", nil })
	if err != nil {
		t.Fatal(err)
	}
}
