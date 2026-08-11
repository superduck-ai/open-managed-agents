package db

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
)

func TestModelCatalogMapperBuilders(t *testing.T) {
	now := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         modelCatalogMapperFindByCatalogKeyStatement,
		bound:             buildModelCatalogMapperFindByCatalogKey(yourbatis.DialectPostgres, modelCatalogTestKey),
		wantID:            "ModelCatalogMapper.FindByCatalogKey",
		wantKind:          yourbatis.StatementSelect,
		wantArgumentNames: []string{"catalogKey"},
		wantSQLFragments:  []string{"FROM model_catalog_snapshots", "WHERE catalog_key = $1"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: modelCatalogMapperSaveSuccessStatement,
		bound: buildModelCatalogMapperSaveSuccess(yourbatis.DialectPostgres, saveModelCatalogSuccessParams{
			CatalogKey:    modelCatalogTestKey,
			Models:        []byte(`[{"id":"provider/model"}]`),
			LastAttemptAt: &now,
			LastSuccessAt: &now,
		}),
		wantID:            "ModelCatalogMapper.SaveSuccess",
		wantKind:          yourbatis.StatementInsert,
		wantArgumentNames: []string{"params.CatalogKey", "params.Models", "params.LastAttemptAt", "params.LastSuccessAt"},
		wantSQLFragments:  []string{"CAST($2 AS jsonb)", "ON CONFLICT (catalog_key)"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: modelCatalogMapperRecordFailureStatement,
		bound: buildModelCatalogMapperRecordFailure(yourbatis.DialectPostgres, recordModelCatalogFailureParams{
			CatalogKey:    modelCatalogTestKey,
			Models:        []byte(`[]`),
			LastAttemptAt: now,
			LastError:     "upstream_timeout",
		}),
		wantID:            "ModelCatalogMapper.RecordFailure",
		wantKind:          yourbatis.StatementInsert,
		wantArgumentNames: []string{"params.CatalogKey", "params.Models", "params.LastAttemptAt", "params.LastError"},
		wantSQLFragments:  []string{"CAST($2 AS jsonb)", "last_error = excluded.last_error"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         modelCatalogMapperDeleteByCatalogKeyStatement,
		bound:             buildModelCatalogMapperDeleteByCatalogKey(yourbatis.DialectPostgres, modelCatalogTestKey),
		wantID:            "ModelCatalogMapper.DeleteByCatalogKey",
		wantKind:          yourbatis.StatementDelete,
		wantArgumentNames: []string{"catalogKey"},
		wantSQLFragments:  []string{"DELETE FROM model_catalog_snapshots"},
	})
}

const modelCatalogTestKey = "default"

func TestModelCatalogSnapshotRoundTripAndFailurePreservesSuccess(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires config: %v", err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Skipf("PostgreSQL integration test requires database: %v", err)
	}
	defer database.Close()

	key := "model_catalog_test_" + uuid.NewString()
	defer func() {
		if err := NewModelCatalogMapper(database.mapperDB).DeleteByCatalogKey(ctx, key); err != nil {
			t.Errorf("DeleteByCatalogKey() error = %v", err)
		}
	}()

	successAt := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	models := json.RawMessage(`[{"id":"provider/model","capabilities":{"thinking":{"supported":true}}}]`)
	if err := database.SaveModelCatalogSuccess(ctx, ModelCatalogSnapshot{
		CatalogKey:    key,
		Models:        models,
		LastAttemptAt: &successAt,
		LastSuccessAt: &successAt,
	}); err != nil {
		t.Fatalf("SaveModelCatalogSuccess() error = %v", err)
	}

	record, exists, err := database.GetModelCatalogSnapshot(ctx, key)
	if err != nil || !exists {
		t.Fatalf("GetModelCatalogSnapshot() = (%+v, %t, %v), want stored row", record, exists, err)
	}
	if !jsonEqual(record.Models, models) || record.LastSuccessAt == nil || !record.LastSuccessAt.Equal(successAt) {
		t.Fatalf("stored snapshot = %+v, want JSON and nullable timestamp round-trip", record)
	}

	attemptedAt := successAt.Add(time.Minute)
	if err := database.RecordModelCatalogFailure(ctx, key, attemptedAt, "upstream_timeout"); err != nil {
		t.Fatalf("RecordModelCatalogFailure() error = %v", err)
	}
	record, exists, err = database.GetModelCatalogSnapshot(ctx, key)
	if err != nil || !exists || record.LastError != "upstream_timeout" {
		t.Fatalf("failure snapshot = (%+v, %t, %v), want failure metadata", record, exists, err)
	}
	if !jsonEqual(record.Models, models) || record.LastSuccessAt == nil || !record.LastSuccessAt.Equal(successAt) {
		t.Fatalf("failure snapshot lost successful data = %+v", record)
	}
}

func jsonEqual(got, want json.RawMessage) bool {
	var gotValue, wantValue any
	if json.Unmarshal(got, &gotValue) != nil || json.Unmarshal(want, &wantValue) != nil {
		return false
	}
	return reflect.DeepEqual(gotValue, wantValue)
}

func TestTryAcquireAdvisoryLockUsesOneSessionAndReleasesIt(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("PostgreSQL integration test requires config: %v", err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Skipf("PostgreSQL integration test requires database: %v", err)
	}
	defer database.Close()

	lockID := time.Now().UnixNano()
	release, acquired, err := database.TryAcquireAdvisoryLock(ctx, lockID)
	if err != nil || !acquired {
		t.Fatalf("first TryAcquireAdvisoryLock() = (%t, %v), want acquired", acquired, err)
	}
	defer release()

	secondRelease, acquired, err := database.TryAcquireAdvisoryLock(ctx, lockID)
	if err != nil || acquired {
		t.Fatalf("second TryAcquireAdvisoryLock() = (%t, %v), want lock contention", acquired, err)
	}
	secondRelease()

	release()
	thirdRelease, acquired, err := database.TryAcquireAdvisoryLock(ctx, lockID)
	if err != nil || !acquired {
		t.Fatalf("third TryAcquireAdvisoryLock() = (%t, %v), want released lock", acquired, err)
	}
	thirdRelease()
}
