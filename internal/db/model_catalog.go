package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type ModelCatalogSnapshot struct {
	CatalogKey    string
	Models        json.RawMessage
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	LastError     string
}

type modelCatalogSnapshotRow struct {
	CatalogKey    string     `db:"catalog_key"`
	Models        []byte     `db:"models"`
	LastAttemptAt *time.Time `db:"last_attempt_at"`
	LastSuccessAt *time.Time `db:"last_success_at"`
	LastError     *string    `db:"last_error"`
}

const getModelCatalogSnapshotSQL = `
	select catalog_key, models, last_attempt_at, last_success_at, last_error
	from model_catalog_snapshots
	where catalog_key = :catalog_key
`

const saveModelCatalogSuccessSQL = `
	insert into model_catalog_snapshots (
		catalog_key, models, last_attempt_at, last_success_at, last_error
	)
	values (
		:catalog_key, CAST(:models AS jsonb), :last_attempt_at, :last_success_at, null
	)
	on conflict (catalog_key) do update
	set models = excluded.models,
		last_attempt_at = excluded.last_attempt_at,
		last_success_at = excluded.last_success_at,
		last_error = null,
		updated_at = now()
`

const recordModelCatalogFailureSQL = `
	insert into model_catalog_snapshots (
		catalog_key, models, last_attempt_at, last_error
	)
	values (
		:catalog_key, CAST(:models AS jsonb), :last_attempt_at, :last_error
	)
	on conflict (catalog_key) do update
	set last_attempt_at = excluded.last_attempt_at,
		last_error = excluded.last_error,
		updated_at = now()
`

func (d *DB) GetModelCatalogSnapshot(ctx context.Context, catalogKey string) (ModelCatalogSnapshot, bool, error) {
	var row modelCatalogSnapshotRow
	err := namedGetContext(ctx, d.sql, &row, getModelCatalogSnapshotSQL, map[string]any{
		"catalog_key": catalogKey,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ModelCatalogSnapshot{}, false, nil
	}
	if err != nil {
		return ModelCatalogSnapshot{}, false, err
	}
	lastError := ""
	if row.LastError != nil {
		lastError = *row.LastError
	}
	return ModelCatalogSnapshot{
		CatalogKey:    row.CatalogKey,
		Models:        append(json.RawMessage(nil), row.Models...),
		LastAttemptAt: cloneModelCatalogTime(row.LastAttemptAt),
		LastSuccessAt: cloneModelCatalogTime(row.LastSuccessAt),
		LastError:     lastError,
	}, true, nil
}

func (d *DB) SaveModelCatalogSuccess(ctx context.Context, snapshot ModelCatalogSnapshot) error {
	_, err := namedExecContext(ctx, d.sql, saveModelCatalogSuccessSQL, map[string]any{
		"catalog_key":     snapshot.CatalogKey,
		"models":          snapshot.Models,
		"last_attempt_at": snapshot.LastAttemptAt,
		"last_success_at": snapshot.LastSuccessAt,
	})
	return err
}

func (d *DB) RecordModelCatalogFailure(ctx context.Context, catalogKey string, attemptedAt time.Time, failure string) error {
	_, err := namedExecContext(ctx, d.sql, recordModelCatalogFailureSQL, map[string]any{
		"catalog_key":     catalogKey,
		"models":          json.RawMessage(`[]`),
		"last_attempt_at": attemptedAt,
		"last_error":      failure,
	})
	return err
}

func cloneModelCatalogTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
