package db

import (
	"context"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestMemoryVersionMapperDeleteOlderThanContract(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	params := deleteMemoryVersionsOlderThanParams{
		WorkspaceUUID:         "workspace-uuid",
		MemoryStoreExternalID: "store-id",
		CreatedBefore:         now.Add(-30 * 24 * time.Hour),
	}
	bound := buildMemoryVersionMapperDeleteOlderThan(yourbatis.DialectPostgres, params)
	assertMapperSQLContains(t, bound, "DELETE FROM memory_versions")
	assertMapperSQLContains(t, bound, "workspace_uuid = $1")
	assertMapperSQLContains(t, bound, "memory_store_external_id = $2")
	assertMapperSQLContains(t, bound, "created_at < $3")
	assertMapperSQLContains(t, bound, "NOT EXISTS")
	assertMapperSQLContains(t, bound, "current_version_external_id = memory_versions.external_id")
	assertMapperSQLContains(t, bound, "deleted_at IS NULL")
}

func TestMemoryVersionMapperDeleteOlderThanExecution(t *testing.T) {
	ctx := context.Background()
	executor := newMapperTestExecutor(t, mapperTestResponse{})
	rows, err := NewMemoryVersionMapper(executor).DeleteOlderThan(ctx, deleteMemoryVersionsOlderThanParams{
		WorkspaceUUID:         "workspace-uuid",
		MemoryStoreExternalID: "store-id",
		CreatedBefore:         time.Now(),
	})
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if rows != 0 {
		t.Fatalf("DeleteOlderThan rows = %d, want 0", rows)
	}
}
