package db

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestTranscriptArchivePostgres(t *testing.T) {
	url := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, url)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	a := TranscriptArchive{UUID: "52000000-0000-0000-0000-000000000090", ExternalID: "tarc_test", OrganizationUUID: "52000000-0000-0000-0000-000000000001", WorkspaceUUID: "52000000-0000-0000-0000-000000000002", CodeSessionUUID: "52000000-0000-0000-0000-000000000003", CodeSessionExternalID: "cs_test", FromSequence: 1, ToSequence: 2, EventCount: 2, Codec: "jsonl+zstd", Bucket: "history", Key: "transcript/test", Size: 10, RawBytes: 20, SHA256: "digest"}
	if err := store.RegisterTranscriptArchive(ctx, a); err != nil {
		t.Fatal(err)
	}
	scope := a.Scope()
	seedTranscriptArchiveSession(t, database, scope)
	seedTranscriptArchiveEvent(t, store, scope, 1, nil, false, time.Now().Add(-8*24*time.Hour))
	seedTranscriptArchiveEvent(t, store, scope, 2, nil, true, time.Now().Add(-8*24*time.Hour))
	foreign := scope
	foreign.WorkspaceUUID = a.OrganizationUUID
	if _, found, err := store.FindTranscriptArchive(ctx, foreign, 1); err != nil || found {
		t.Fatalf("foreign lookup: %t %v", found, err)
	}
	if err := store.AttachTranscriptArchive(ctx, foreign, a.UUID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("foreign attach: %v", err)
	}
	batch := TranscriptDeleteBatch{Scope: scope, ArchiveUUID: a.UUID, Sequences: []int64{1}, Cutoff: time.Now(), Eligibility: TranscriptArchiveQuery{Cutoff: time.Now().Add(-7 * 24 * time.Hour)}}
	if _, err := store.SoftDeleteInternalEventsBatch(ctx, batch); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("pending deletion: %v", err)
	}
	duplicate := a
	duplicate.UUID = "52000000-0000-0000-0000-000000000091"
	duplicate.ExternalID = "tarc_duplicate"
	if err := store.RegisterTranscriptArchive(ctx, duplicate); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate range: %v, want ErrDuplicate", err)
	}
	rollback := errors.New("rollback")
	err := store.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		if _, err := NewTranscriptArchiveMapper(tx).Attach(ctx, scope, a.UUID); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	got, found, err := store.FindTranscriptArchive(ctx, scope, 1)
	if err != nil || !found || got.State != "pending" {
		t.Fatalf("rollback: %+v %v", got, err)
	}
	if err := store.AttachTranscriptArchive(ctx, scope, a.UUID); err != nil {
		t.Fatal(err)
	}
	if err := store.AttachTranscriptArchive(ctx, scope, a.UUID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("reattach: %v", err)
	}
	archives, err := store.ListTranscriptArchives(ctx, scope, 0, 10, true)
	if err != nil || len(archives) != 1 {
		t.Fatalf("list: %d %v", len(archives), err)
	}
	// T4 rechecks eligibility in every batch, even with an attached manifest.
	ineligible := batch
	ineligible.Eligibility.Terminal = true
	if count, err := store.SoftDeleteInternalEventsBatch(ctx, ineligible); err != nil || count != 0 {
		t.Fatalf("non-terminal batch: %d %v", count, err)
	}
	ineligible = batch
	ineligible.Eligibility.Cutoff = time.Now().Add(-9 * 24 * time.Hour)
	if count, err := store.SoftDeleteInternalEventsBatch(ctx, ineligible); err != nil || count != 0 {
		t.Fatalf("too-recent batch: %d %v", count, err)
	}
	// Failed batches must leave the eligible row untouched, including when a
	// valid sequence appears before an uncovered sequence in the same request.
	denied := batch
	denied.Scope = foreign
	if _, err := store.SoftDeleteInternalEventsBatch(ctx, denied); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("foreign soft delete: %v", err)
	}
	denied = batch
	denied.Sequences = []int64{1, 3}
	if _, err := store.SoftDeleteInternalEventsBatch(ctx, denied); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("mixed coverage: %v", err)
	}
	query := TranscriptArchiveQuery{Scope: scope, ToSequence: 2, Limit: 10}
	rows, err := store.ReadTranscriptArchiveRange(ctx, query)
	if err != nil || len(rows) != 2 || rows[0].DeletedAt != nil {
		t.Fatalf("failed batch changed rows: %+v %v", rows, err)
	}
	if count, err := store.SoftDeleteInternalEventsBatch(ctx, batch); err != nil || count != 1 {
		t.Fatalf("soft delete: %d %v, want 1", count, err)
	}
	rows, err = store.ReadTranscriptArchiveRange(ctx, query)
	if err != nil || len(rows) != 2 || rows[0].DeletedAt == nil || rows[1].DeletedAt != nil {
		t.Fatalf("soft delete state: %+v %v", rows, err)
	}
	if count, err := store.SoftDeleteInternalEventsBatch(ctx, batch); err != nil || count != 0 {
		t.Fatalf("repeat soft delete: %d %v", count, err)
	}
	// Strict cutoff: an event deleted exactly at cutoff is still retained.
	batch.Cutoff = *rows[0].DeletedAt
	if count, err := store.HardDeleteInternalEventsBatch(ctx, batch); err != nil || count != 0 {
		t.Fatalf("observation window: %d %v", count, err)
	}
	batch.Cutoff = batch.Cutoff.Add(time.Microsecond)
	if count, err := store.HardDeleteInternalEventsBatch(ctx, batch); err != nil || count != 1 {
		t.Fatalf("hard delete: %d %v, want 1", count, err)
	}
	rows, err = store.ReadTranscriptArchiveRange(ctx, query)
	if err != nil || len(rows) != 1 || rows[0].SequenceNum != 2 || rows[0].DeletedAt != nil {
		t.Fatalf("hard delete state: %+v %v", rows, err)
	}
	if count, err := store.HardDeleteInternalEventsBatch(ctx, batch); err != nil || count != 0 {
		t.Fatalf("repeat hard delete: %d %v", count, err)
	}
	batch.Sequences = []int64{3}
	if _, err := store.HardDeleteInternalEventsBatch(ctx, batch); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("uncovered delete: %v", err)
	}
	batch.Sequences = nil
	if _, err := store.SoftDeleteInternalEventsBatch(ctx, batch); !errors.Is(err, ErrLimitExceeded) {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "update transcript_archives set updated_at=now()-interval '2 days'"); err != nil {
		t.Fatal(err)
	}
	if err := store.ScheduleTranscriptArchiveCleanup(ctx, 10); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.LeaseObjectCleanupJobs(ctx, "archive-test", 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("attached cleanup: %d %v", len(jobs), err)
	}
	// Verify the schema rollback guard without discarding the only archive copy.
	if _, err := provider.Down(ctx); err == nil {
		t.Fatal("attached migration rollback accepted")
	}
}
