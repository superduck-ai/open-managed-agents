package db

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStampDSHPermissionEventsRejectsInvalidPayload(t *testing.T) {
	for _, payload := range []string{`{`, `null`, `[]`} {
		t.Run(payload, func(t *testing.T) {
			if _, err := stampDSHPermissionEvents([]SessionEvent{{Payload: json.RawMessage(payload)}}, time.Now().UTC().Truncate(time.Microsecond)); err == nil {
				t.Fatal("invalid permission payload accepted")
			}
		})
	}
}

func TestStampDSHPermissionEventsPreservesPayloadAndInput(t *testing.T) {
	raw := json.RawMessage(`{"input":{"integer":9007199254740993},"processed_at":"2000-01-01T00:00:00Z"}`)
	stale := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	input := []SessionEvent{{Payload: raw, CreatedAt: stale, ProcessedAt: stale}, {Payload: raw, CreatedAt: stale, ProcessedAt: stale}}
	stamped, err := stampDSHPermissionEvents(input, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range stamped {
		var payload struct {
			Input       json.RawMessage `json:"input"`
			ProcessedAt time.Time       `json:"processed_at"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if string(payload.Input) != `{"integer":9007199254740993}` {
			t.Fatalf("numeric precision lost: %s", payload.Input)
		}
		if !event.ProcessedAt.After(stale) || !payload.ProcessedAt.Equal(event.ProcessedAt) || !event.CreatedAt.Equal(stamped[0].ProcessedAt) || event.ProcessedAt.Nanosecond()%1000 != 0 {
			t.Fatal("permission copies have inconsistent timestamps")
		}
		if string(input[i].Payload) != string(raw) || !input[i].ProcessedAt.Equal(stale) || !input[i].CreatedAt.Equal(stale) {
			t.Fatal("stamping mutated caller input")
		}
	}
}
