package sessions

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
	"time"
)

func TestConfirmationQueuedUntilWorkerProcesses(t *testing.T) {
	now := time.Now().UTC()
	event, _, _, err := normalizeInputEvent(db.Session{}, json.RawMessage(`{"type":"user.tool_confirmation","tool_use_id":"actual-use","result":"allow"}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if !event.ProcessedAt.IsZero() {
		t.Fatal("confirmation falsely processed on HTTP receipt")
	}
	var queued map[string]any
	if err = json.Unmarshal(sessionEventPayloadForResponse(event, "thread"), &queued); err != nil {
		t.Fatal(err)
	}
	if v, ok := queued["processed_at"]; !ok || v != nil {
		t.Fatalf("queued response=%v", queued)
	}
	event.ProcessedAt = now
	var processed map[string]any
	if err = json.Unmarshal(sessionEventPayloadForResponse(event, "thread"), &processed); err != nil {
		t.Fatal(err)
	}
	if processed["processed_at"] != formatEventTime(now) || processed["id"] != queued["id"] || processed["tool_use_id"] != "actual-use" || processed["result"] != "allow" {
		t.Fatalf("processed authority/identity/time=%v", processed)
	}
}
