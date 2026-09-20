package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptretention"
)

func TestTranscriptArchiveMissingObjectRecovery(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-recovery")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 3))
	makeArchiveTerminal(t, app, session)
	service := transcriptretention.New(app.db, objects, transcriptPolicy(), nil)
	scope := transcriptScope(session)
	objects.uploadErr = errors.New("failed before object creation")
	if err := service.Archive(t.Context(), scope, true); err == nil {
		t.Fatal("upload failure accepted")
	}
	objects.uploadErr = nil
	archives, err := app.db.ListTranscriptArchives(t.Context(), scope, 0, 10, false)
	if err != nil || len(archives) != 1 {
		t.Fatalf("pending archives: %v, %v", archives, err)
	}
	orphan := archives[0]
	if _, err := objects.Open(t.Context(), orphan.Key, nil); err == nil {
		t.Fatal("failed upload created an object")
	}
	if err := service.Archive(t.Context(), scope, true); err == nil {
		t.Fatal("missing object accepted")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 3)
	worker := cleanup.NewWorker(app.db, newFakeStorageClient(objects), 0, nil)
	if err := worker.RunOnce(t.Context(), "archive-recovery"); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='pending'", 1)
	if _, err := app.pool.Exec(t.Context(), "update transcript_archives set created_at=now()-interval '2 days', updated_at=now()-interval '2 days' where uuid=$1", orphan.UUID); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(t.Context(), "archive-recovery"); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='deleting'", 1)
	assertPayloadSQLCount(t, app, "select count(*) from jobs where type='object_cleanup' and status='completed'", 1)
	if len(objects.deleteOptions) != 1 || !objects.deleteOptions[0].AllVersions {
		t.Fatal("archive cleanup must delete all object versions")
	}
	if err := service.Archive(t.Context(), scope, true); err != nil {
		t.Fatal(err)
	}
	archives, err = app.db.ListTranscriptArchives(t.Context(), scope, 0, 10, true)
	if err != nil || len(archives) != 1 || archives[0].UUID == orphan.UUID || archives[0].Key == orphan.Key {
		t.Fatalf("archive not rebuilt with a new identity: %v, %v", archives, err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 0)
	if _, err := app.pool.Exec(t.Context(), "update transcript_archives set updated_at=now()-interval '2 days' where state='attached'"); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(t.Context(), "archive-recovery"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReadSegment(t.Context(), archives[0]); err != nil {
		t.Fatal(err)
	}
	if len(objects.deleteOptions) != 1 {
		t.Fatal("cleanup claimed an attached archive")
	}
}

type archiveReadCountingStore struct {
	*payloadFaultStore
	reads map[string]int
}

func (s *archiveReadCountingStore) Open(ctx context.Context, key string, byteRange *storage.ByteRange) (storage.Object, error) {
	s.reads[key]++
	return s.payloadFaultStore.Open(ctx, key, byteRange)
}

func TestTranscriptArchiveReadsObjectsOncePerAttempt(t *testing.T) {
	objects := &archiveReadCountingStore{payloadFaultStore: &payloadFaultStore{fakeStore: newFakeStore("archive-reads")}, reads: make(map[string]int)}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, []db.AppendCodeSessionInternalEventInput{{Payload: []byte(sizedPrivatePayload("blob", 65536))}})
	makeArchiveTerminal(t, app, session)
	service := transcriptretention.New(app.db, objects, transcriptPolicy(), nil)
	clear(objects.reads)
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	if len(objects.reads) != 2 {
		t.Fatalf("read %d objects, want payload and archive", len(objects.reads))
	}
	for key, count := range objects.reads {
		if count != 1 {
			t.Fatalf("object %s read %d times", key, count)
		}
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 0)
}
