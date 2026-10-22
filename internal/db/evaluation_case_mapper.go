package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationCaseMapper -sql ./evaluation_case_mapper.xml -out ./evaluation_case_mapper.sqlmap.gen.go -dialect postgres

type evaluationCaseRow struct {
	UUID               string    `db:"uuid"`
	ExternalID         string    `db:"external_id"`
	OrganizationUUID   string    `db:"organization_uuid"`
	WorkspaceUUID      string    `db:"workspace_uuid"`
	DatasetUUID        string    `db:"dataset_uuid"`
	VersionUUID        string    `db:"version_uuid"`
	Position           int       `db:"position"`
	Name               string    `db:"name"`
	Description        string    `db:"description"`
	TurnsJSON          string    `db:"turns_json"`
	CheckpointsJSON    *string   `db:"checkpoints_json"`
	InputFilesJSON     *string   `db:"input_files_json"`
	ReferenceFilesJSON *string   `db:"reference_files_json"`
	CreatedAt          time.Time `db:"created_at"`
	UpdatedAt          time.Time `db:"updated_at"`
}

type evaluationCaseWriteParams struct {
	UUID               string
	ExternalID         string
	OrganizationUUID   string
	WorkspaceUUID      string
	DatasetUUID        string
	VersionUUID        string
	Position           int
	Name               string
	Description        string
	TurnsJSON          string
	CheckpointsJSON    *string
	InputFilesJSON     *string
	ReferenceFilesJSON *string
	CreatedAt          time.Time
}

type evaluationCaseUpdateParams struct {
	OrganizationUUID   string
	WorkspaceUUID      string
	VersionUUID        string
	ExternalID         string
	Name               string
	Description        string
	TurnsJSON          string
	CheckpointsJSON    *string
	InputFilesJSON     *string
	ReferenceFilesJSON *string
	UpdatedAt          time.Time
}

type evaluationCasePageParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	VersionUUID      string
	FetchLimit       int
	Cursor           *EvaluationCasePageCursor
}

type EvaluationCaseMapper interface {
	MaxPosition(ctx context.Context, organizationUUID, workspaceUUID, versionUUID string) (int, error)
	Insert(ctx context.Context, params evaluationCaseWriteParams) (evaluationCaseRow, error)
	Update(ctx context.Context, params evaluationCaseUpdateParams) (evaluationCaseRow, error)
	Delete(ctx context.Context, organizationUUID, workspaceUUID, versionUUID, externalID string) (evaluationCaseRow, error)
	ListPage(ctx context.Context, params evaluationCasePageParams) ([]evaluationCaseRow, error)
	ListByVersion(ctx context.Context, organizationUUID, workspaceUUID, versionUUID string) ([]evaluationCaseRow, error)
}
