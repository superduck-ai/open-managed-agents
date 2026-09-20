package tests

import (
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestTranscriptArchivePhysicalDelete(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-delete")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 5))
	makeArchiveTerminal(t, app, session)
	policy := transcriptPolicy()
	service := newTranscriptRetentionService(t, app, objects, policy)
	scope := transcriptScope(session)
	if err := service.Archive(t.Context(), scope, true); err != nil {
		t.Fatal(err)
	}
	if err := service.HardDelete(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 5)
	policy.HardDeleteEnabled = true
	service = newTranscriptRetentionService(t, app, objects, policy)
	if err := service.HardDelete(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 5)
	if _, err := app.pool.Exec(t.Context(), "update code_session_internal_events set deleted_at=now()-interval '15 days'"); err != nil {
		t.Fatal(err)
	}
	saved := objects.objects
	objects.objects = make(map[string]fakeObject)
	if err := service.HardDelete(t.Context(), scope); err == nil {
		t.Fatal("missing backup permitted deletion")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 5)
	objects.objects = saved
	if _, err := app.pool.Exec(t.Context(), "update transcript_archives set state='deleting'"); err != nil {
		t.Fatal(err)
	}
	if err := service.HardDelete(t.Context(), scope); err == nil {
		t.Fatal("deleting manifest permitted deletion")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 5)
	if _, err := app.pool.Exec(t.Context(), "update transcript_archives set state='attached'"); err != nil {
		t.Fatal(err)
	}
	policy.MaxRowsPerJob = 2
	service = newTranscriptRetentionService(t, app, objects, policy)
	if err := service.HardDelete(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 3)
	policy.DryRun = true
	service = newTranscriptRetentionService(t, app, objects, policy)
	if err := service.HardDelete(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 3)
	policy.DryRun = false
	policy.SoftDeleteWindow = 0
	policy.MaxRowsPerJob = 50000
	service = newTranscriptRetentionService(t, app, objects, policy)
	if err := service.HardDelete(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 0)
	if err := service.HardDelete(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	if err := app.db.ScheduleTranscriptArchiveCleanup(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='attached'", 1)
	scopes, err := app.db.ListTranscriptDeletionCandidates(t.Context(), "", time.Now(), 100)
	if err != nil || len(scopes) != 0 {
		t.Fatalf("deletion candidates: %d %v", len(scopes), err)
	}
}
