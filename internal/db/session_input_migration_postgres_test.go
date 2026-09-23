package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestSessionInputIndexMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 64); err != nil {
		t.Fatal(err)
	}
	assertMigrationColumnExists(t, ctx, database, "code_sessions", "worker_turn_started", true)
	assertMigrationColumnNullable(t, ctx, database, "session_events", "processed_at", "YES")
	var exists bool
	if err := database.QueryRowContext(ctx, `SELECT to_regclass('session_events_processed_order_idx') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("state migration must not create the index: exists=%t err=%v", exists, err)
	}

	// A writer makes the concurrent build wait after creating an invalid index.
	writer, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := writer.ExecContext(ctx, `UPDATE session_events SET processed_at = processed_at WHERE false`); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := provider.UpTo(blocked, 65); err == nil || blocked.Err() == nil {
		t.Fatalf("expected index build to time out waiting for the writer: %v", err)
	}
	if err := writer.Rollback(); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := database.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = 'session_events_processed_order_idx'::regclass`).Scan(&valid); err != nil || valid {
		t.Fatalf("interrupted build must leave an invalid index: valid=%t err=%v", valid, err)
	}
	if _, err := provider.UpTo(ctx, 65); err != nil {
		t.Fatalf("retry interrupted index migration: %v", err)
	}
	if err := database.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = 'session_events_processed_order_idx'::regclass`).Scan(&valid); err != nil || !valid {
		t.Fatalf("retry must create a valid index: valid=%t err=%v", valid, err)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT to_regclass('session_events_processed_order_idx') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("index migration rollback: exists=%t err=%v", exists, err)
	}
	assertMigrationColumnExists(t, ctx, database, "code_sessions", "worker_turn_started", true)
	assertMigrationColumnNullable(t, ctx, database, "session_events", "processed_at", "YES")

	// Earlier PR revisions already created this index in migration 64.
	if _, err := database.ExecContext(ctx, `CREATE INDEX session_events_processed_order_idx ON session_events (workspace_uuid, session_external_id, processed_at, id) WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 65); err != nil {
		t.Fatalf("upgrade with the earlier PR index: %v", err)
	}
}
