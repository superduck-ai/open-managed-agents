package db

import (
	"encoding/json"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

func TestMemoryStoreBatchPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	storeDB := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	workspaceUUID := uuid.NewV4().String()
	otherWorkspaceUUID := uuid.NewV4().String()
	for _, id := range []string{"active", "archived", "deleted", "other-workspace"} {
		workspace := workspaceUUID
		if id == "other-workspace" {
			workspace = otherWorkspaceUUID
		}
		_, err := storeDB.CreateMemoryStore(ctx, MemoryStore{
			UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: uuid.NewV4().String(),
			WorkspaceUUID: workspace, Name: id, Metadata: json.RawMessage(`{"purpose":"batch-test"}`), CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := storeDB.ArchiveMemoryStore(ctx, workspaceUUID, "archived"); err != nil {
		t.Fatal(err)
	}
	// Fixture for the legacy soft-deleted state excluded by all store reads.
	if _, err := database.ExecContext(ctx, "UPDATE memory_stores SET deleted_at = now() WHERE external_id = 'deleted'"); err != nil {
		t.Fatal(err)
	}
	t.Run("missing deleted and foreign stores are omitted", func(t *testing.T) {
		rows, err := storeDB.GetMemoryStoresByExternalIDs(ctx, workspaceUUID, []string{"missing", "deleted", "other-workspace"})
		if err != nil || len(rows) != 0 {
			t.Fatalf("rows = %v, error = %v", rows, err)
		}
	})
	t.Run("batch includes archived and scans JSON and nullable creator", func(t *testing.T) {
		rows, err := storeDB.GetMemoryStoresByExternalIDs(ctx, workspaceUUID, []string{"archived", "active", "active"})
		if err != nil || len(rows) != 2 {
			t.Fatalf("rows = %v, error = %v", rows, err)
		}
		for _, row := range rows {
			var metadata struct {
				Purpose string `json:"purpose"`
			}
			if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.Purpose != "batch-test" || row.CreatedByAPIKeyUUID != "" {
				t.Fatalf("unexpected row: %+v", row)
			}
			if (row.ExternalID == "archived") != (row.ArchivedAt != nil) {
				t.Fatalf("archive state = %+v", row)
			}
		}
	})
}
