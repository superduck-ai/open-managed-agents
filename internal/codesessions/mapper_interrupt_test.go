package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPublicInterruptHistoryDoesNotBecomeLiveControl(t *testing.T) {
	payload, err := workerPayloadForPublicEvent("cse_stop", json.RawMessage(`{"type":"user.interrupt"}`), "stored_uuid", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := BuildEventMetadata("cse_stop", "inbound", payload)
	if err != nil || metadata.EventType != "user.interrupt" {
		t.Fatalf("historical stop must retain its original conversion: %+v %v", metadata, err)
	}
}

func TestPublicInterruptMapsToStableWorkerControlRequest(t *testing.T) {
	event := db.SessionEvent{UUID: "stored_uuid", EventType: "user.interrupt", Payload: json.RawMessage(`{"type":"user.interrupt","id":"public_event"}`), ProcessedAt: time.Now().UTC()}
	for range 2 {
		payload, err := workerPayloadForQueuedPublicEvent("cse_stop", event)
		if err != nil {
			t.Fatal(err)
		}
		var request struct {
			Type      string `json:"type"`
			UUID      string `json:"uuid"`
			SessionID string `json:"session_id"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Fatal(err)
		}
		if request.Type != "control_request" || request.Request.Subtype != "interrupt" || request.UUID != event.UUID || request.RequestID != event.UUID || request.SessionID != "cse_stop" {
			t.Fatalf("public interrupt did not produce a stable Worker interrupt: %s", payload)
		}
		metadata, err := BuildEventMetadata("cse_stop", "inbound", payload)
		if err != nil || metadata.EventType != "control_request" || metadata.EventSubtype != "interrupt" {
			t.Fatalf("interrupt must select the reply lane: %+v %v", metadata, err)
		}
	}
}
