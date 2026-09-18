package tests

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptretention"
)

func TestTranscriptArchiveSoftAndHardVisibility(t *testing.T) {
	for _, hard := range []bool{false, true} {
		name := "soft"
		if hard {
			name = "hard"
		}
		t.Run(name, func(t *testing.T) {
			objects := &payloadFaultStore{fakeStore: newFakeStore("archive-visibility")}
			app := newPayloadIntegrationApp(t, objects)
			session, _ := newPayloadIntegrationSession(t, app)
			seedArchiveEvents(t, app, session, []db.AppendCodeSessionInternalEventInput{{}, {IsCompaction: true}, {}})
			scope := transcriptScope(session)
			query := db.TranscriptArchiveQuery{Scope: scope, ToSequence: 3, Limit: 10}
			original, err := app.db.ReadTranscriptArchiveRange(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			before := transcriptHTTPBytes(t, app, session.ExternalID, "internal-events")
			policy := transcriptPolicy()
			policy.HardDeleteEnabled = true
			policy.SoftDeleteWindow = 0
			service := transcriptretention.New(app.db, objects, policy, nil)
			if err := service.Archive(t.Context(), scope, false); err != nil {
				t.Fatal(err)
			}
			// Inline data supports direct observation-window rollback without blob GC.
			if _, err := app.pool.Exec(t.Context(), "update code_session_internal_events set deleted_at=null"); err != nil {
				t.Fatal(err)
			}
			restored, err := app.db.ReadTranscriptArchiveRange(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original, restored) {
				t.Fatal("soft-delete rollback changed persistence bytes")
			}
			if err := service.Archive(t.Context(), scope, false); err != nil {
				t.Fatal(err)
			}
			if hard {
				if err := service.HardDelete(t.Context(), scope); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(before, transcriptHTTPBytes(t, app, session.ExternalID, "internal-events")) {
				t.Fatal("deletion changed visible boundary or response")
			}
			old := original[0]
			input := db.AppendCodeSessionInternalEventInput{ExternalID: old.ExternalID + "_retry", Payload: old.Payload, PayloadUUID: old.PayloadUUID, PayloadHash: old.PayloadHash, IdempotencyKey: old.IdempotencyKey, EventMetadata: old.EventMetadata, EventType: old.EventType, CreatedAt: old.CreatedAt}
			if _, err := app.db.AppendCodeSessionInternalEvents(t.Context(), session.ExternalID, session.CurrentWorkerEpoch, []db.AppendCodeSessionInternalEventInput{input}); err != nil {
				t.Fatal(err)
			}
			assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 3)
		})
	}
}
