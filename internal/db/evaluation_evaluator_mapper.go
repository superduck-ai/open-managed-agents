package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationEvaluatorMapper -sql ./evaluation_evaluator_mapper.xml -out ./evaluation_evaluator_mapper.sqlmap.gen.go -dialect postgres

type evaluationEvaluatorRow struct {
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

type evaluationEvaluatorPageRow struct {
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
	EvaluatorType     string     `db:"evaluator_type"`
	Model             string     `db:"model"`
	AgentID           string     `db:"agent_id"`
	PromptTemplate    string     `db:"prompt_template"`
	VariablesJSON     string     `db:"variables_json"`
	VersionCreatedAt  time.Time  `db:"version_created_at"`
}

type evaluationEvaluatorWriteParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	Name              string
	Description       string
	CreatedByUserUUID *string
	CreatedAt         time.Time
}

type evaluationEvaluatorPageParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	FetchLimit       int
	Cursor           *EvaluationEvaluatorPageCursor
}

type EvaluationEvaluatorMapper interface {
	Insert(ctx context.Context, params evaluationEvaluatorWriteParams) (evaluationEvaluatorRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationEvaluatorPageRow, error)
	FindForUpdate(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationEvaluatorRow, error)
	ListPage(ctx context.Context, params evaluationEvaluatorPageParams) ([]evaluationEvaluatorPageRow, error)
}
