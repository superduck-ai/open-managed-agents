package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestMemoryStoreBatchQuery(t *testing.T) {
	ctx := context.Background()
	t.Run("propagates query failure", func(t *testing.T) {
		expected := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: expected})
		_, err := NewMemoryStoreMapper(executor).FindByExternalIDs(ctx, "workspace", []string{"missing"})
		if !errors.Is(err, expected) {
			t.Fatalf("error = %v, want query failure", err)
		}
	})
	t.Run("empty IDs need no database", func(t *testing.T) {
		stores, err := (&DB{}).GetMemoryStoresByExternalIDs(ctx, "workspace", nil)
		if err != nil || len(stores) != 0 {
			t.Fatalf("stores = %v, error = %v", stores, err)
		}
	})
	t.Run("binds IDs and scans archived and keyless stores in one query", func(t *testing.T) {
		first := memoryStoreMapperTestRow()
		second := memoryStoreMapperTestRow()
		second[1] = "archived-store"
		second[4] = nil
		second[10] = time.Now().UTC()
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: memoryStoreMapperTestColumns(), rows: [][]driver.Value{second, first},
		})
		rows, err := NewMemoryStoreMapper(executor).FindByExternalIDs(ctx, "workspace", []string{"store-id", "archived-store"})
		if err != nil || len(rows) != 2 {
			t.Fatalf("rows = %v, error = %v", rows, err)
		}
		if rows[0].ArchivedAt == nil || rows[0].CreatedByAPIKeyUUID != nil {
			t.Fatalf("nullable fields = %+v", rows[0])
		}
		if executor.queryCallCount != 1 {
			t.Fatalf("queries = %d, want 1", executor.queryCallCount)
		}
		assertMapperTestExecution(t, executor, "MemoryStoreMapper.FindByExternalIDs", yourbatis.StatementSelect, []any{"workspace", "store-id", "archived-store"})
	})
}

func TestMemoryStoreBatchQuerySQL(t *testing.T) {
	bound := buildMemoryStoreMapperFindByExternalIDs(yourbatis.DialectPostgres, "workspace", []string{"store-a", "store-b"})
	assertMemoryMapperContract(t, memoryMapperContract{
		statement: memoryStoreMapperFindByExternalIDsStatement, bound: bound,
		id: "MemoryStoreMapper.FindByExternalIDs", kind: yourbatis.StatementSelect,
		argumentNames: []string{"workspaceUUID", "externalID", "externalID"},
		fragments:     []string{"workspace_uuid = $1", "deleted_at IS NULL", "external_id IN", "$2", "$3"},
	})
}
