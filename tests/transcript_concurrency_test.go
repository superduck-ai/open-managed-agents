package tests

import (
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestTranscriptArchiveConcurrentRetry(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-concurrent")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 4))
	makeArchiveTerminal(t, app, session)
	uploaded := make(chan struct{})
	release := make(chan struct{})
	objects.afterUpload = func(string) error {
		close(uploaded)
		select {
		case <-release:
			return nil
		case <-time.After(10 * time.Second):
			return t.Context().Err()
		}
	}
	service := newTranscriptRetentionService(t, app, objects, transcriptPolicy())
	done := make(chan error, 1)
	go func() { done <- service.Archive(t.Context(), transcriptScope(session), true) }()
	select {
	case <-uploaded:
	case <-time.After(10 * time.Second):
		t.Fatal("upload timeout")
	}
	secondErr := service.Archive(t.Context(), transcriptScope(session), true)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives", 1)
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is not null", 4)
}

func TestTranscriptArchiveLargeBatchTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("large PostgreSQL fixture")
	}
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-large")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 1))
	makeArchiveTerminal(t, app, session)
	_, err := app.pool.Exec(t.Context(), `insert into code_session_internal_events
 (external_id,organization_uuid,workspace_uuid,code_session_uuid,code_session_external_id,sequence_num,event_type,payload_uuid,payload,payload_hash,idempotency_key,event_metadata,created_at,updated_at)
 select 'load_'||g,organization_uuid,workspace_uuid,code_session_uuid,code_session_external_id,g,event_type,'payload_'||g,payload,payload_hash,'key_'||g,event_metadata,created_at,updated_at
 from code_session_internal_events cross join generate_series(2,500000) g where sequence_num=1`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.pool.Exec(t.Context(), `create function reject_third_archive_batch() returns trigger language plpgsql as $$ begin
 if new.sequence_num>1000 and new.deleted_at is not null then raise exception 'injected third batch failure'; end if; return new; end $$;
 create trigger reject_archive_batch before update on code_session_internal_events for each row execute function reject_third_archive_batch()`)
	if err != nil {
		t.Fatal(err)
	}
	policy := transcriptPolicy()
	policy.MaxRowsPerJob = 1500
	policy.DeleteBatchRows = 500
	service := newTranscriptRetentionService(t, app, objects, policy)
	if err := service.Archive(t.Context(), transcriptScope(session), true); err == nil {
		t.Fatal("third batch failure ignored")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is not null", 1000)
	if _, err := app.pool.Exec(t.Context(), "drop trigger reject_archive_batch on code_session_internal_events"); err != nil {
		t.Fatal(err)
	}
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	// The retry finishes 500 existing rows and archives 1000 new rows within its 1500-row budget.
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is not null", 2500)
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 500000)
}
