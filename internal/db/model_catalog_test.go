package db

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestModelCatalogQueriesUseNamedPostgreSQLBindings(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		name      string
		query     string
		arguments map[string]any
		wantJSON  bool
	}{
		{
			name:  "failure metadata",
			query: recordModelCatalogFailureSQL,
			arguments: map[string]any{
				"catalog_key":     modelCatalogTestKey,
				"models":          json.RawMessage(`[]`),
				"last_attempt_at": now,
				"last_error":      "upstream_timeout",
			},
			wantJSON: true,
		},
		{
			name:  "successful snapshot",
			query: saveModelCatalogSuccessSQL,
			arguments: map[string]any{
				"catalog_key":     modelCatalogTestKey,
				"models":          json.RawMessage(`[{"id":"provider/model"}]`),
				"last_attempt_at": now,
				"last_success_at": now,
			},
			wantJSON: true,
		},
		{
			name:  "snapshot lookup",
			query: getModelCatalogSnapshotSQL,
			arguments: map[string]any{
				"catalog_key": modelCatalogTestKey,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query, values, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if strings.Contains(query, ":catalog_key") || len(values) == 0 {
				t.Fatalf("bound query = %q, values = %#v", query, values)
			}
			if test.wantJSON && !strings.Contains(query, "CAST($") {
				t.Fatalf("bound query = %q, want PostgreSQL JSON cast", query)
			}
		})
	}
}

const modelCatalogTestKey = "default"

func TestModelCatalogSnapshotSQLXRoundTripAndFailurePreservesSuccess(t *testing.T) {
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
		_, _ = database.sql.ExecContext(ctx, `delete from model_catalog_snapshots where catalog_key = $1`, key)
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
