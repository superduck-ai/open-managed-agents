package tests

import (
	"bytes"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestTranscriptArchiveRestoreAfterBlobGC(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-restore")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, []db.AppendCodeSessionInternalEventInput{{Payload: []byte(sizedPrivatePayload("large", 65536))}, {IsCompaction: true}, {}})
	scope := transcriptScope(session)
	policy := transcriptPolicy()
	policy.HardDeleteEnabled = true
	policy.SoftDeleteWindow = 0
	service := newTranscriptRetentionService(t, app, objects, policy)
	var before bytes.Buffer
	if err := service.Export(t.Context(), scope, &before); err != nil {
		t.Fatal(err)
	}
	query := db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, CodeSessionExternalID: session.ExternalID, Limit: 500}
	initial, _, err := app.db.ListCodeSessionInternalEventsPage(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	makeArchiveTerminal(t, app, session)
	if err := service.Archive(t.Context(), scope, true); err != nil {
		t.Fatal(err)
	}
	if err := service.HardDelete(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 0)
	if _, err := app.pool.Exec(t.Context(), "update event_payload_blobs set updated_at=now()-interval '2 days'"); err != nil {
		t.Fatal(err)
	}
	if err := app.db.ScheduleEventPayloadCleanup(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from event_payload_blobs where state='deleting'", 1)
	for key := range objects.objects {
		if strings.HasPrefix(key, "event-payload/") {
			delete(objects.objects, key)
		}
	}
	var archived bytes.Buffer
	if err := service.Export(t.Context(), scope, &archived); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Bytes(), archived.Bytes()) {
		t.Fatal("complete exported history changed after deletion")
	}
	if err := service.Restore(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	if err := service.Restore(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	restored, _, err := app.db.ListCodeSessionInternalEventsPage(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial) != len(restored) {
		t.Fatalf("ListPage count after restore: %d", len(restored))
	}
	for i := range initial {
		if initial[i].UUID != restored[i].UUID || initial[i].SequenceNum != restored[i].SequenceNum || !bytes.Equal(initial[i].Payload, restored[i].Payload) {
			t.Fatal("ListPage changed after restore")
		}
	}
	var after bytes.Buffer
	if err := service.Export(t.Context(), scope, &after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatal("restored history changed")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 3)
	foreign := scope
	foreign.WorkspaceUUID = scope.OrganizationUUID
	var empty bytes.Buffer
	if err := service.Export(t.Context(), foreign, &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Len() != 0 {
		t.Fatal("foreign export leaked history")
	}
}
