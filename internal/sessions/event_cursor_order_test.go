package sessions

import (
	"encoding/base64"
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestEventCursorBindsAscendingOrder(t *testing.T) {
	raw := encodeEventCursor(db.SessionEvent{ExternalID: "event-test"}, "asc")
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["order"] != "asc" {
		t.Fatalf("cursor loses ascending sort order: %s", data)
	}
}

func TestEventCursorRejectsChangingOrder(t *testing.T) {
	for _, original := range []string{"asc", "desc"} {
		cursor := encodeEventCursor(db.SessionEvent{ExternalID: "event-test"}, original)
		for _, requested := range []string{"asc", "desc"} {
			got, err := decodeEventCursor(cursor, requested)
			if (err == nil) != (original == requested) {
				t.Fatalf("%s cursor reused as %s: %v", original, requested, err)
			}
			if err == nil && (got == nil || got.ExternalID != "event-test") {
				t.Fatal("cursor lost event position")
			}
		}
	}
}
func TestEventCursorLegacyDefaultAndInvalidOrder(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString([]byte(`{"external_id":"event-test"}`))
	if _, err := decodeEventCursor(raw, "desc"); err == nil {
		t.Fatal("legacy default asc cursor crossed order")
	}
	if _, err := decodeEventCursor(raw, "asc"); err != nil {
		t.Fatal("legacy ascending cursor broken", err)
	}
	for _, order := range []string{"invalid", "ASC"} {
		raw := base64.RawURLEncoding.EncodeToString([]byte(`{"external_id":"event-test","order":"` + order + `"}`))
		if _, err := decodeEventCursor(raw, "asc"); err == nil {
			t.Fatal("invalid encoded order accepted")
		}
	}
}
