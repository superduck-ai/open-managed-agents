package codesessions

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestWorkerPauseRetainsToolIDsAcrossPublicNormalization(t *testing.T) {
	at := time.Date(2026, 10, 17, 10, 16, 41, 0, time.UTC)
	for _, pending := range [][]string{{"settled-tool"}, {"primary-tool", "child-tool"}, nil} {
		payload := workerStatusPublicPayload("session.status_idle", "pause", at, pending)
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Stop struct {
				Type string   `json:"type"`
				IDs  []string `json:"event_ids"`
			} `json:"stop_reason"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.ID != "pause" || event.Type != "session.status_idle" || event.Stop.Type != "requires_action" || !reflect.DeepEqual(event.Stop.IDs, pending) {
			t.Fatalf("tool pause identity lost: %s", raw)
		}
	}
	running := workerStatusPublicPayload("session.status_running", "resume", at, nil)
	if _, exists := running["stop_reason"]; exists {
		t.Fatal("running reported a terminal stop reason")
	}
}
