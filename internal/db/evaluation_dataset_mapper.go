package db

import (
	"context"
	"encoding/json"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationDatasetMapper -sql ./evaluation_dataset_mapper.xml -out ./evaluation_dataset_mapper.sqlmap.gen.go -dialect postgres

type evaluationDatasetRow struct {
	UUID              string    `db:"uuid"`
	ExternalID        string    `db:"external_id"`
	OrganizationUUID  string    `db:"organization_uuid"`
	WorkspaceUUID     string    `db:"workspace_uuid"`
	Name              string    `db:"name"`
	Description       string    `db:"description"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
	VersionUUID       string    `db:"version_uuid"`
	VersionExternalID string    `db:"version_external_id"`
	Version           int       `db:"version"`
	CaseCount         int       `db:"case_count"`
}

type evaluationDatasetLockRow struct {
	UUID       string `db:"uuid"`
	ExternalID string `db:"external_id"`
}

type evaluationDatasetVersionRow struct {
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	WorkspaceUUID    string    `db:"workspace_uuid"`
	DatasetUUID      string    `db:"dataset_uuid"`
	Version          int       `db:"version"`
	CreatedAt        time.Time `db:"created_at"`
}

type evaluationCaseRow struct {
	UUID              string    `db:"uuid"`
	ExternalID        string    `db:"external_id"`
	OrganizationUUID  string    `db:"organization_uuid"`
	WorkspaceUUID     string    `db:"workspace_uuid"`
	DatasetUUID       string    `db:"dataset_uuid"`
	VersionUUID       string    `db:"version_uuid"`
	Position          int       `db:"position"`
	Query             string    `db:"query"`
	ExpectedAnswer    string    `db:"expected_answer"`
	ExpectedToolTrace []byte    `db:"expected_tool_trace"`
	SourceSessionID   string    `db:"source_session_id"`
	Polarity          string    `db:"polarity"`
	Acceptance        string    `db:"acceptance"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

type insertEvaluationDatasetParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	Name             string
	Description      string
	CreatedAt        time.Time
}

type insertEvaluationDatasetVersionParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	DatasetUUID      string
	Version          int
	CreatedAt        time.Time
}

type insertEvaluationCaseParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	DatasetUUID       string
	VersionUUID       string
	Position          int
	Query             string
	ExpectedAnswer    string
	ExpectedToolTrace json.RawMessage
	SourceSessionID   string
	Polarity          string
	Acceptance        string
	CreatedAt         time.Time
}

type updateEvaluationCaseParams struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	DatasetUUID       string
	ExternalID        string
	Query             string
	ExpectedAnswer    string
	ExpectedToolTrace json.RawMessage
	SourceSessionID   string
	Polarity          string
	Acceptance        string
	UpdatedAt         time.Time
}

type evaluationListParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	Limit            int
	Offset           int
}

type EvaluationDatasetMapper interface {
	InsertDataset(ctx context.Context, params insertEvaluationDatasetParams) (evaluationDatasetLockRow, error)
	InsertVersion(ctx context.Context, params insertEvaluationDatasetVersionParams) (evaluationDatasetVersionRow, error)
	InsertCase(ctx context.Context, params insertEvaluationCaseParams) (evaluationCaseRow, error)
	LockDataset(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationDatasetLockRow, error)
	FindLatestVersion(ctx context.Context, datasetUUID string) (evaluationDatasetVersionRow, error)
	MaxCasePosition(ctx context.Context, versionUUID string) (int, error)
	FindDataset(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationDatasetRow, error)
	ListDatasets(ctx context.Context, params evaluationListParams) ([]evaluationDatasetRow, error)
	SoftDeleteDataset(ctx context.Context, organizationUUID, workspaceUUID, externalID string, deletedAt time.Time) (int64, error)
	ListCases(ctx context.Context, organizationUUID, workspaceUUID, versionUUID string) ([]evaluationCaseRow, error)
	UpdateCase(ctx context.Context, params updateEvaluationCaseParams) (evaluationCaseRow, error)
	SoftDeleteCase(ctx context.Context, organizationUUID, workspaceUUID, datasetUUID, externalID string, deletedAt time.Time) (int64, error)
}
