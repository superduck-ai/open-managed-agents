package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationDatasetMapper -sql ./evaluation_dataset_mapper.xml -out ./evaluation_dataset_mapper.sqlmap.gen.go -dialect postgres

type evaluationDatasetRow struct {
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	Name              string     `db:"name"`
	Description       string     `db:"description"`
	CreatedByUserUUID *string    `db:"created_by_user_uuid"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	ArchivedAt        *time.Time `db:"archived_at"`
}

type evaluationDatasetPageRow struct {
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	Name              string     `db:"name"`
	Description       string     `db:"description"`
	CreatedByUserUUID *string    `db:"created_by_user_uuid"`
	CreatedAt         time.Time  `db:"created_at"`
	UpdatedAt         time.Time  `db:"updated_at"`
	ArchivedAt        *time.Time `db:"archived_at"`
	VersionUUID       string     `db:"version_uuid"`
	VersionExternalID string     `db:"version_external_id"`
	VersionNumber     int        `db:"version_number"`
	VersionCreatedAt  time.Time  `db:"version_created_at"`
	CaseCount         int64      `db:"case_count"`
}

type evaluationDatasetWriteParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	Name              string
	Description       string
	CreatedByUserUUID *string
	CreatedAt         time.Time
}

type evaluationDatasetPageParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	FetchLimit       int
	Cursor           *EvaluationDatasetPageCursor
}

type EvaluationDatasetMapper interface {
	Insert(ctx context.Context, params evaluationDatasetWriteParams) (evaluationDatasetRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationDatasetPageRow, error)
	FindForUpdate(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationDatasetRow, error)
	ListPage(ctx context.Context, params evaluationDatasetPageParams) ([]evaluationDatasetPageRow, error)
}
