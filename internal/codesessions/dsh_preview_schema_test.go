package codesessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestDSRPreviewDoesNotExposePublicationMetadata(t *testing.T) {
	for _, raw := range []string{`{"id":"internal","processed_at":"invalid","type":"preview.start","event":{"id":"final","type":"agent.message"}}`, `{"id":"internal","processed_at":"invalid","type":"preview.delta","event_id":"final","delta":{"type":"content_delta"}}`} {
		batch, previews, err := prepareDSHPublicEvents(db.DSHScope{}, []json.RawMessage{json.RawMessage(raw)})
		if err != nil || len(batch) != 0 || len(previews) != 1 {
			t.Fatalf("conversion: %v", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(previews[0], &fields); err != nil {
			t.Fatal(err)
		}
		if fields["id"] != nil || fields["processed_at"] != nil {
			t.Fatalf("internal metadata leaked: %s", previews[0])
		}
		if fields["event"] == nil && string(fields["event_id"]) != `"final"` {
			t.Fatalf("target lost: %s", previews[0])
		}
	}
}
