package db

import (
	"context"
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
	foreign := scope
	foreign.WorkspaceUUID = a.OrganizationUUID
	if _, found, err := store.FindTranscriptArchive(ctx, foreign, 1); err != nil || found {
		t.Fatalf("foreign lookup: %t %v", found, err)
	}
	if err := store.AttachTranscriptArchive(ctx, foreign, a.UUID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("foreign attach: %v", err)
	}
	batch := TranscriptDeleteBatch{Scope: scope, ArchiveUUID: a.UUID, Sequences: []int64{1}, Cutoff: time.Now()}
	if _, err := store.SoftDeleteInternalEventsBatch(ctx, batch); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("pending deletion: %v", err)
	}
	duplicate := a
	duplicate.UUID = "52000000-0000-0000-0000-000000000091"
	duplicate.ExternalID = "tarc_duplicate"
	if err := store.RegisterTranscriptArchive(ctx, duplicate); err == nil {
		t.Fatal("duplicate range accepted")
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
	for _, hard := range []bool{false, true} {
		var err error
		if hard {
			_, err = store.HardDeleteInternalEventsBatch(ctx, batch)
		} else {
			_, err = store.SoftDeleteInternalEventsBatch(ctx, batch)
		}
		if err != nil {
			t.Fatal(err)
		}
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
	query := TranscriptArchiveQuery{Scope: scope, Limit: 10, ToSequence: 2, Cutoff: time.Now()}
	for _, terminal := range []bool{false, true} {
		query.Terminal = terminal
		testTranscriptQueries(t, ctx, store, query)
	}
}

func testTranscriptQueries(t *testing.T, ctx context.Context, store *DB, query TranscriptArchiveQuery) {
	t.Helper()
	if _, err := store.ListArchivableTranscriptSessions(ctx, query); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListArchivableInternalEvents(ctx, query); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadTranscriptArchiveRange(ctx, query); err != nil {
		t.Fatal(err)
	}
}
