package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationDatasetVersionMapper -sql ./evaluation_dataset_version_mapper.xml -out ./evaluation_dataset_version_mapper.sqlmap.gen.go -dialect postgres

type evaluationDatasetVersionRow struct {
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	WorkspaceUUID    string    `db:"workspace_uuid"`
	DatasetUUID      string    `db:"dataset_uuid"`
	Version          int       `db:"version"`
	CreatedAt        time.Time `db:"created_at"`
}

type evaluationDatasetVersionWriteParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	DatasetUUID      string
	Version          int
	CreatedAt        time.Time
}

type EvaluationDatasetVersionMapper interface {
	Insert(ctx context.Context, params evaluationDatasetVersionWriteParams) (evaluationDatasetVersionRow, error)
	FindLatest(ctx context.Context, organizationUUID, workspaceUUID, datasetUUID string) (evaluationDatasetVersionRow, error)
}
