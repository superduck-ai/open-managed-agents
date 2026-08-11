package db

import (
	"context"
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

func (d *DB) GetModelCatalogSnapshot(ctx context.Context, catalogKey string) (ModelCatalogSnapshot, bool, error) {
	if d == nil || d.mapperDB == nil {
		return ModelCatalogSnapshot{}, false, errors.New("database is not configured")
	}
	row, found, err := NewModelCatalogMapper(d.mapperDB).FindByCatalogKey(ctx, catalogKey)
	if err != nil {
		return ModelCatalogSnapshot{}, false, err
	}
	if !found {
		return ModelCatalogSnapshot{}, false, nil
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
	if d == nil || d.mapperDB == nil {
		return errors.New("database is not configured")
	}
	return NewModelCatalogMapper(d.mapperDB).SaveSuccess(ctx, saveModelCatalogSuccessParams{
		CatalogKey:    snapshot.CatalogKey,
		Models:        snapshot.Models,
		LastAttemptAt: snapshot.LastAttemptAt,
		LastSuccessAt: snapshot.LastSuccessAt,
	})
}

func (d *DB) RecordModelCatalogFailure(ctx context.Context, catalogKey string, attemptedAt time.Time, failure string) error {
	if d == nil || d.mapperDB == nil {
		return errors.New("database is not configured")
	}
	return NewModelCatalogMapper(d.mapperDB).RecordFailure(ctx, recordModelCatalogFailureParams{
		CatalogKey:    catalogKey,
		Models:        json.RawMessage(`[]`),
		LastAttemptAt: attemptedAt,
		LastError:     failure,
	})
}

func cloneModelCatalogTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
