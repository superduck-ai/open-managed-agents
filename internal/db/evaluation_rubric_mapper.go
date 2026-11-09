package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationRubricMapper -sql ./evaluation_rubric_mapper.xml -out ./evaluation_rubric_mapper.sqlmap.gen.go -dialect postgres

type evaluationRubricRow struct {
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
	Body              string    `db:"body"`
	PassScore         int       `db:"pass_score"`
	Model             string    `db:"model"`
}

type evaluationRubricLockRow struct {
	UUID       string `db:"uuid"`
	ExternalID string `db:"external_id"`
}

type evaluationRubricVersionRow struct {
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	WorkspaceUUID    string    `db:"workspace_uuid"`
	RubricUUID       string    `db:"rubric_uuid"`
	Version          int       `db:"version"`
	Body             string    `db:"body"`
	PassScore        int       `db:"pass_score"`
	Model            string    `db:"model"`
	CreatedAt        time.Time `db:"created_at"`
}

type insertEvaluationRubricParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	Name             string
	Description      string
	CreatedAt        time.Time
}

type insertEvaluationRubricVersionParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	RubricUUID       string
	Version          int
	Body             string
	PassScore        int
	Model            string
	CreatedAt        time.Time
}

type EvaluationRubricMapper interface {
	InsertRubric(ctx context.Context, params insertEvaluationRubricParams) (evaluationRubricLockRow, error)
	InsertRubricVersion(ctx context.Context, params insertEvaluationRubricVersionParams) (evaluationRubricVersionRow, error)
	LockRubric(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationRubricLockRow, error)
	FindLatestRubricVersion(ctx context.Context, rubricUUID string) (evaluationRubricVersionRow, error)
	FindRubric(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationRubricRow, error)
	ListRubrics(ctx context.Context, params evaluationListParams) ([]evaluationRubricRow, error)
	SoftDeleteRubric(ctx context.Context, organizationUUID, workspaceUUID, externalID string, deletedAt time.Time) (int64, error)
	TouchRubric(ctx context.Context, rubricUUID string, updatedAt time.Time) (int64, error)
}
