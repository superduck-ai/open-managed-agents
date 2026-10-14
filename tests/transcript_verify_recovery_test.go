package tests

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestVerifyTranscriptRecovery(t *testing.T) {
	requireTranscriptVerification(t)
	started := time.Now()
	for _, test := range []struct {
		name, stage string
		run         func(*testing.T)
	}{
		{"upload failures", "upload_failures_recovered", verifyTranscriptUploadRecovery},
		{"archive batch failure", "archive_batches_recovered", verifyTranscriptArchiveBatchRecovery},
		{"delete batch failure", "delete_batches_recovered", verifyTranscriptDeleteBatchRecovery},
		{"restore batch failure", "restore_batches_recovered", verifyTranscriptRestoreBatchRecovery},
	} {
		if !t.Run(test.name, test.run) {
			t.Fatal("recovery assertion failed")
		}
		transcriptVerifyProof(t, started, test.stage)
	}
}

func verifyTranscriptUploadRecovery(t *testing.T) {
	for _, after := range []bool{false, true} {
		name := "before upload"
		if after {
			name = "after upload"
		}
		t.Run(name, func(t *testing.T) {
			f := newTranscriptVerification(t, 4, false)
			interrupted := func() error { return errors.New("injected upload interruption") }
			if after {
				f.objects.afterUpload = interrupted
			} else {
				f.objects.beforeUpload = interrupted
			}
			if err := f.service.Archive(t.Context(), f.scope, true); err == nil {
				t.Fatal("upload interruption accepted")
			}
			f.rows(t, f.count, f.count)
			pending := f.segment(t, "pending")
			f.objects.beforeUpload, f.objects.afterUpload = nil, nil
			if !after {
				if err := f.service.Archive(t.Context(), f.scope, true); !errors.Is(err, storage.ErrNotFound) {
					t.Fatalf("pending without object must reject retry: %v", err)
				}
				f.rows(t, f.count, f.count)
				worker := cleanup.NewWorker(f.app.db, f.client, 0, nil)
				if err := worker.RunOnce(t.Context(), "verify-pending-young"); err != nil {
					t.Fatal(err)
				}
				f.segment(t, "pending")
				if _, err := f.app.pool.Exec(t.Context(), "update transcript_archives set created_at=now()-interval '2 days',updated_at=now()-interval '2 days' where uuid=$1", pending.UUID); err != nil {
					t.Fatal(err)
				}
				if err := worker.RunOnce(t.Context(), "verify-pending-expired"); err != nil {
					t.Fatal(err)
				}
				assertPayloadSQLCount(t, f.app, "select count(*) from transcript_archives where uuid=$1 and state='deleting'", 1, pending.UUID)
				assertPayloadSQLCount(t, f.app, "select count(*) from jobs where type='object_cleanup' and status='completed'", 1)
			}
			f.archive(t)
			f.archive(t)
			attached := f.segment(t, "attached")
			if after && (attached.UUID != pending.UUID || attached.Key != pending.Key || f.objects.uploads.Load() != 1) {
				t.Fatal("retry replaced or reuploaded a completed object")
			}
			if !after && (attached.UUID == pending.UUID || attached.Key == pending.Key || f.objects.uploads.Load() != 2) {
				t.Fatal("expired pending was not rebuilt with a new identity")
			}
			f.rows(t, f.count, 0)
			f.assertExport(t)
		})
	}
}

func transcriptBatchFault(t *testing.T, f *transcriptVerification, operation, condition string) func() {
	t.Helper()
	sql := "create function reject_verify_transcript_batch() returns trigger language plpgsql as $$ begin if " + condition + " then raise exception 'injected transcript batch failure'; end if; return "
	result := "new"
	if operation == "delete" {
		result = "old"
	}
	sql += result + "; end $$; create trigger reject_verify_transcript_batch before " + operation + " on code_session_internal_events for each row execute function reject_verify_transcript_batch()"
	if _, err := f.app.pool.Exec(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := f.app.pool.Exec(t.Context(), "drop trigger reject_verify_transcript_batch on code_session_internal_events"); err != nil {
			t.Fatal(err)
		}
	}
}

func verifyTranscriptArchiveBatchRecovery(t *testing.T) {
	f := newTranscriptVerification(t, 4, false)
	remove := transcriptBatchFault(t, f, "update", "new.sequence_num>2 and new.deleted_at is not null")
	if err := f.service.Archive(t.Context(), f.scope, true); err == nil {
		t.Fatal("soft delete failure accepted")
	}
	f.rows(t, 4, 2)
	attached := f.segment(t, "attached")
	f.assertExport(t)
	remove()
	f.archive(t)
	f.archive(t)
	if f.segment(t, "attached").UUID != attached.UUID || f.objects.uploads.Load() != 1 {
		t.Fatal("soft delete retry replaced archive")
	}
	f.rows(t, 4, 0)
	f.assertExport(t)
}

func verifyTranscriptDeleteBatchRecovery(t *testing.T) {
	f := newTranscriptVerification(t, 4, false)
	f.archive(t)
	f.ageDeleted(t)
	remove := transcriptBatchFault(t, f, "delete", "old.sequence_num>2")
	if err := f.service.HardDelete(t.Context(), f.scope); err == nil {
		t.Fatal("physical delete failure accepted")
	}
	f.rows(t, 2, 0)
	f.assertExport(t)
	remove()
	f.hardDelete(t)
	f.hardDelete(t)
	f.rows(t, 0, 0)
	f.assertExport(t)
	if err := f.service.Restore(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}
	f.rows(t, 4, 4)
	f.assertExport(t)
}

func verifyTranscriptRestoreBatchRecovery(t *testing.T) {
	f := newTranscriptVerification(t, 501, false)
	before := transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events")
	f.archive(t)
	f.ageDeleted(t)
	f.hardDelete(t)
	f.rows(t, 0, 0)
	remove := transcriptBatchFault(t, f, "insert", "new.sequence_num>500")
	if err := f.service.Restore(t.Context(), f.scope); err == nil {
		t.Fatal("restore failure accepted")
	}
	f.rows(t, 500, 500)
	remove()
	for range 2 {
		if err := f.service.Restore(t.Context(), f.scope); err != nil {
			t.Fatal(err)
		}
	}
	f.rows(t, 501, 501)
	f.assertExport(t)
	if !bytes.Equal(before, transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events")) {
		t.Fatal("restore retry changed HTTP history")
	}
	records, err := f.app.db.ReadTranscriptArchiveRange(t.Context(), db.TranscriptArchiveQuery{Scope: f.scope, ToSequence: 501, Limit: 502})
	if err != nil || len(records) != 501 {
		t.Fatalf("restore retry lost rows: %v", err)
	}
	for i, record := range records {
		if record.SequenceNum != int64(i+1) {
			t.Fatal("restore retry duplicated or reordered rows")
		}
	}
}
