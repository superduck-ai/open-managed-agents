package db

import (
	"encoding/json"
	"testing"
)

func TestArchiveDoesNotMirrorAnExistingPrimaryStatusCopy(t *testing.T) {
	primary := SessionThread{UUID: "primary-uuid", ExternalID: "primary"}
	child := SessionThread{UUID: "child-uuid", ExternalID: "child"}
	source := SessionEvent{UUID: "source-uuid", ExternalID: "event-child", EventType: "session.thread_status_terminated", ThreadExternalID: &child.ExternalID, StatusThreadID: child.ExternalID, Payload: []byte(`{"id":"event-child","type":"session.thread_status_terminated","session_thread_id":"child"}`)}
	events, err := sessionThreadStatusCopies(source, primary, child)
	if err != nil || len(events) != 2 {
		t.Fatalf("status insertion copies=%d err=%v", len(events), err)
	}
	if archivedThreadNeedsPrimaryCopy(events[1], primary) {
		t.Fatal("archive would mirror the primary copy into a second primary event")
	}
	if !archivedThreadNeedsPrimaryCopy(events[0], primary) {
		t.Fatal("child source must retain its primary delivery")
	}
	copy, err := archivedThreadPrimaryCopy(events[0], primary)
	if err != nil || copy.ExternalID != events[1].ExternalID {
		t.Fatal("archive must reuse the existing deterministic primary event identity")
	}
	for _, eventType := range []string{"session.thread_status_running", "session.thread_status_idle", "session.thread_created"} {
		source.EventType = eventType
		if archivedThreadNeedsPrimaryCopy(source, primary) {
			t.Fatal("archive projected an unrelated event", eventType)
		}
	}
}

func TestArchivedThreadPrimaryCopyRetainsTargetWithDistinctStableEventIdentity(t *testing.T) {
	child := "child"
	original := SessionEvent{UUID: "original-uuid", ExternalID: "event-child", EventType: "session.thread_status_terminated", ThreadExternalID: &child, StatusThreadID: child, Payload: []byte(`{"id":"event-child","type":"session.thread_status_terminated","session_thread_id":"child","agent_name":"worker","processed_at":"2026-10-19T14:00:00Z"}`)}
	primary := SessionThread{ExternalID: "primary", UUID: "primary-uuid"}
	got, err := archivedThreadPrimaryCopy(original, primary)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if json.Unmarshal(got.Payload, &p) != nil {
		t.Fatal("primary event JSON")
	}
	if got.ExternalID == original.ExternalID || got.UUID == original.UUID || got.ThreadExternalID == nil || *got.ThreadExternalID != "primary" || got.ThreadUUID == nil || *got.ThreadUUID != "primary-uuid" || got.StatusThreadID != "child" || p["session_thread_id"] != "child" || p["original_event_id"] != original.ExternalID || p["id"] != got.ExternalID || p["agent_name"] != "worker" {
		t.Fatalf("invalid primary mirror: %+v %s", got, got.Payload)
	}
	replay, err := archivedThreadPrimaryCopy(original, primary)
	if err != nil || replay.ExternalID != got.ExternalID {
		t.Fatal("unstable mirror event identity")
	}
	if *original.ThreadExternalID != "child" {
		t.Fatal("source mutated")
	}
	original.Payload = []byte(`invalid`)
	if _, err := archivedThreadPrimaryCopy(original, primary); err == nil {
		t.Fatal("bad public payload accepted")
	}
}
