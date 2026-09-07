package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestWorkerRequestRestoreUsesCommitOrderDespiteReversedSourceClocks(t *testing.T) {
	worker := db.CodeSession{ExternalID: "worker", CurrentWorkerEpoch: 7}
	raw, err := json.Marshal(storedWorkerRequest{RequestID: "request", Epoch: 7})
	if err != nil {
		t.Fatal(err)
	}
	start := db.SessionEvent{
		ExternalID: maevents.ModelRequestEventID(worker.ExternalID, "request", "start"),
		EventType:  "span.model_request_start", Payload: raw,
		ProcessedAt: time.Unix(1, 0), CreatedAt: time.Unix(20, 0),
	}
	end := db.SessionEvent{
		ExternalID: maevents.ModelRequestEventID(worker.ExternalID, "request", "end"),
		EventType:  "span.model_request_end", Payload: raw,
		ProcessedAt: time.Unix(2, 0), CreatedAt: time.Unix(10, 0),
	}
	state := &workerRequestContext{primary: "primary", active: map[string]string{}, known: map[string]bool{}, scopes: map[string]bool{}}
	// The activation query returns created_at order: end before start. Only the
	// server's committed processed_at ordering can safely reconstruct activity.
	if err := state.restore(worker, []db.SessionEvent{end, start}); err != nil {
		t.Fatal(err)
	}
	if len(state.active) != 0 || !state.known["request"] {
		t.Fatalf("closed request reopened during recovery: active=%v known=%v", state.active, state.known)
	}
}
