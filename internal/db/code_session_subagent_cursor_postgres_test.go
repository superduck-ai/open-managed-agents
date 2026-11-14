//go:build integration

package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

const (
	cursorTestOrganization = "54000000-0000-0000-0000-000000000001"
	cursorTestWorkspace    = "54000000-0000-0000-0000-000000000002"
	cursorOtherWorkspace   = "54000000-0000-0000-0000-000000000003"
)

func seedCursorCodeSession(t *testing.T, ctx context.Context, database *sql.DB, externalID, sessionUUID string, lastSequence int64) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		insert into code_sessions (
			external_id, organization_uuid, workspace_uuid, session_uuid, session_external_id,
			environment_uuid, environment_external_id, metadata, current_worker_epoch, last_internal_sequence_num
		) values ($1, $2, $3, $4, $5, '54000000-0000-0000-0000-000000000004', 'env_cursor', '{}', 1, $6)`,
		externalID, cursorTestOrganization, cursorTestWorkspace, sessionUUID, "sesn_"+externalID, lastSequence,
	); err != nil {
		t.Fatalf("seed code session: %v", err)
	}
}

func TestSubagentPublicationProgressPostgres(t *testing.T) {
	url := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, url)
	if _, err := provider.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	seedCursorCodeSession(t, ctx, database, "cse_cursor_existing", "54000000-0000-0000-0001-000000000001", 7)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	seedCursorCodeSession(t, ctx, database, "cse_cursor_new", "54000000-0000-0000-0001-000000000002", 0)
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}

	if _, err := store.SubagentPublicationProgress(ctx, cursorOtherWorkspace, "cse_cursor_existing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace read error = %v, want ErrNotFound", err)
	}
	if _, err := store.SubagentPublicationProgress(ctx, cursorTestWorkspace, "cse_cursor_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing read error = %v, want ErrNotFound", err)
	}
	assertSubagentPublicationProgress(t, ctx, store, "cse_cursor_existing", SubagentPublicationProgress{PublishedSequenceNum: 0, CommittedSequenceNum: 7})
	if err := store.AdvanceSubagentPublishedSequence(ctx, cursorOtherWorkspace, "cse_cursor_existing", 50); err != nil {
		t.Fatal(err)
	}
	assertSubagentPublicationProgress(t, ctx, store, "cse_cursor_existing", SubagentPublicationProgress{PublishedSequenceNum: 0, CommittedSequenceNum: 7})
	if err := store.AdvanceSubagentPublishedSequence(ctx, cursorTestWorkspace, "cse_cursor_existing", 5); err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceSubagentPublishedSequence(ctx, cursorTestWorkspace, "cse_cursor_existing", 3); err != nil {
		t.Fatal(err)
	}
	assertSubagentPublicationProgress(t, ctx, store, "cse_cursor_existing", SubagentPublicationProgress{PublishedSequenceNum: 5, CommittedSequenceNum: 7})
	if err := store.AdvanceSubagentPublishedSequence(ctx, cursorTestWorkspace, "cse_cursor_existing", 7); err != nil {
		t.Fatal(err)
	}
	assertSubagentPublicationProgress(t, ctx, store, "cse_cursor_existing", SubagentPublicationProgress{PublishedSequenceNum: 7, CommittedSequenceNum: 7})
	assertSubagentPublicationProgress(t, ctx, store, "cse_cursor_new", SubagentPublicationProgress{})

	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := database.QueryRowContext(ctx, `select exists (
		select 1 from information_schema.columns
		where table_schema = current_schema() and table_name = 'code_sessions' and column_name = 'subagent_published_sequence_num')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("down migration kept subagent_published_sequence_num")
	}
}

func assertSubagentPublicationProgress(t *testing.T, ctx context.Context, store *DB, codeSessionID string, want SubagentPublicationProgress) {
	t.Helper()
	got, err := store.SubagentPublicationProgress(ctx, cursorTestWorkspace, codeSessionID)
	if err != nil || got != want {
		t.Fatalf("SubagentPublicationProgress(%s) = %+v, %v; want %+v", codeSessionID, got, err, want)
	}
}
