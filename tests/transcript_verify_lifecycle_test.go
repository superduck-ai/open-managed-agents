package tests

import (
	"bytes"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestVerifyTranscriptLifecycle(t *testing.T) {
	f := newTranscriptVerification(t, 36, true)
	rows, err := f.app.db.ReadTranscriptArchiveRange(t.Context(), db.TranscriptArchiveQuery{Scope: f.scope, ToSequence: int64(f.count), Limit: 100})
	if err != nil || len(rows) != f.count || rows[0].PayloadBlobUUID == nil {
		t.Fatalf("large-payload fixture: %v", err)
	}
	blob, err := f.app.db.GetEventPayloadBlob(t.Context(), f.scope.WorkspaceUUID, *rows[0].PayloadBlobUUID)
	if err != nil {
		t.Fatal(err)
	}
	beforeHTTP := transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events")
	cli := f.maintenanceCLI(t)
	f.archive(t)
	f.rows(t, f.count, 0)
	segment := f.segment(t, "attached")
	f.assertExport(t)
	exported, err := cli("export", f.scope)
	if err != nil || !bytes.Equal(exported, f.before) {
		t.Fatalf("archive CLI export differs: %v", err)
	}
	transcriptVerifyProof(t, f.started, "archive_export_matches")
	f.hardDelete(t)
	f.rows(t, f.count, 0)
	f.ageDeleted(t)
	f.hardDelete(t)
	f.hardDelete(t)
	f.rows(t, 0, 0)
	f.assertExport(t)
	if _, err := f.service.ReadSegment(t.Context(), segment); err != nil {
		t.Fatal(err)
	}
	transcriptVerifyProof(t, f.started, "hard_delete_preserves_backup")
	if _, err := f.app.pool.Exec(t.Context(), "update event_payload_blobs set updated_at=now()-interval '2 days' where uuid=$1", blob.UUID); err != nil {
		t.Fatal(err)
	}
	worker := cleanup.NewWorker(f.app.db, f.client, 0, nil)
	if err := worker.RunOnce(t.Context(), "verify-transcript-gc"); err != nil {
		t.Fatal(err)
	}
	object, err := f.objects.Open(t.Context(), blob.Key, nil)
	if err == nil {
		object.Body.Close()
		t.Fatal("old external payload survived real object cleanup")
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("old blob absence: %v", err)
	}
	f.assertExport(t)
	transcriptVerifyProof(t, f.started, "blob_gc_completed")
	for range 2 {
		if _, err := cli("restore", f.scope); err != nil {
			t.Fatal(err)
		}
	}
	f.rows(t, f.count, f.count)
	f.assertExport(t)
	if !bytes.Equal(beforeHTTP, transcriptHTTPBytes(t, f.app, f.session.ExternalID, "internal-events")) {
		t.Fatal("restored private HTTP history differs")
	}
	exported, err = cli("export", f.scope)
	if err != nil || !bytes.Equal(exported, f.before) {
		t.Fatalf("restored CLI export differs: %v", err)
	}
	var watermark int64
	if err := f.app.pool.QueryRow(t.Context(), "select last_internal_sequence_num from code_sessions where uuid=$1", f.scope.CodeSessionUUID).Scan(&watermark); err != nil || watermark != int64(f.count) {
		t.Fatalf("restore changed append watermark: %d %v", watermark, err)
	}
	transcriptVerifyProof(t, f.started, "cli_restore_matches")
	foreign := f.scope
	foreign.WorkspaceUUID = foreign.OrganizationUUID
	exported, err = cli("export", foreign)
	if err != nil || len(exported) != 0 {
		t.Fatalf("foreign export exposed history: %v", err)
	}
	if _, err := cli("restore", foreign); err != nil {
		t.Fatal(err)
	}
	f.rows(t, f.count, f.count)
	f.assertExport(t)
	transcriptVerifyProof(t, f.started, "tenant_history_isolated")
}
