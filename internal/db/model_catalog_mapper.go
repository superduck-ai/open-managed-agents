package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ModelCatalogMapper -sql ./model_catalog_mapper.xml -out ./model_catalog_mapper.sqlmap.gen.go -dialect postgres

type modelCatalogSnapshotRow struct {
	CatalogKey    string     `db:"catalog_key"`
	Models        []byte     `db:"models"`
	LastAttemptAt *time.Time `db:"last_attempt_at"`
	LastSuccessAt *time.Time `db:"last_success_at"`
	LastError     *string    `db:"last_error"`
}

type saveModelCatalogSuccessParams struct {
	CatalogKey    string
	Models        []byte
	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
}

type recordModelCatalogFailureParams struct {
	CatalogKey    string
	Models        []byte
	LastAttemptAt time.Time
	LastError     string
}

type ModelCatalogMapper interface {
	FindByCatalogKey(ctx context.Context, catalogKey string) (modelCatalogSnapshotRow, bool, error)
	SaveSuccess(ctx context.Context, params saveModelCatalogSuccessParams) error
	RecordFailure(ctx context.Context, params recordModelCatalogFailureParams) error
	DeleteByCatalogKey(ctx context.Context, catalogKey string) error
}
