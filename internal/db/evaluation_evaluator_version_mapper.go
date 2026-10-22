package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationEvaluatorVersionMapper -sql ./evaluation_evaluator_version_mapper.xml -out ./evaluation_evaluator_version_mapper.sqlmap.gen.go -dialect postgres

type evaluationEvaluatorVersionRow struct {
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	WorkspaceUUID    string    `db:"workspace_uuid"`
	EvaluatorUUID    string    `db:"evaluator_uuid"`
	Version          int       `db:"version"`
	EvaluatorType    string    `db:"evaluator_type"`
	Model            string    `db:"model"`
	AgentID          string    `db:"agent_id"`
	PromptTemplate   string    `db:"prompt_template"`
	VariablesJSON    string    `db:"variables_json"`
	CreatedAt        time.Time `db:"created_at"`
}

type evaluationEvaluatorVersionWriteParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	EvaluatorUUID    string
	Version          int
	EvaluatorType    string
	Model            string
	AgentID          string
	PromptTemplate   string
	VariablesJSON    string
	CreatedAt        time.Time
}

type EvaluationEvaluatorVersionMapper interface {
	Insert(ctx context.Context, params evaluationEvaluatorVersionWriteParams) (evaluationEvaluatorVersionRow, error)
	FindScopedByExternalID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationEvaluatorVersionRow, error)
	FindLatest(ctx context.Context, organizationUUID, workspaceUUID, evaluatorUUID string) (evaluationEvaluatorVersionRow, error)
}
