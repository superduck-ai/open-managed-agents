package tests

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestTranscriptArchiveOrphanCleanup(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-gc")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 3))
	makeArchiveTerminal(t, app, session)
	service := newTranscriptRetentionService(t, app, objects, transcriptPolicy())
	objects.afterUpload = func(string) error { return errors.New("lost confirmation") }
	if err := service.Archive(t.Context(), transcriptScope(session), true); err == nil {
		t.Fatal("injected failure ignored")
	}
	worker := cleanup.NewWorker(app.db, newFakeStorageClient(objects), 0, nil)
	if err := worker.RunOnce(t.Context(), "archive-gc"); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='pending'", 1)
	if _, err := app.pool.Exec(t.Context(), "update transcript_archives set updated_at=now()-interval '25 hours'"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(t.Context(), "alter table jobs add constraint fail_archive_cleanup check (type <> 'object_cleanup')"); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(t.Context(), "archive-gc"); err == nil {
		t.Fatal("enqueue failure ignored")
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='pending'", 1)
	if _, err := app.pool.Exec(t.Context(), "alter table jobs drop constraint fail_archive_cleanup"); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(t.Context(), "archive-gc"); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 0 || len(objects.deleteOptions) != 1 || !objects.deleteOptions[0].AllVersions {
		t.Fatal("orphan object not fully deleted")
	}
	objects.afterUpload = nil
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='attached'", 1)
	if _, err := app.pool.Exec(t.Context(), "update transcript_archives set updated_at=now()-interval '25 hours'"); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(t.Context(), "archive-gc"); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 1 {
		t.Fatal("attached backup deleted")
	}
}
