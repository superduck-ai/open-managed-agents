package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationExperimentBindingMapper -sql ./evaluation_experiment_binding_mapper.xml -out ./evaluation_experiment_binding_mapper.sqlmap.gen.go -dialect postgres

type evaluationExperimentBindingRow struct {
	UUID                string    `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	OrganizationUUID    string    `db:"organization_uuid"`
	WorkspaceUUID       string    `db:"workspace_uuid"`
	ExperimentUUID      string    `db:"experiment_uuid"`
	Position            int       `db:"position"`
	EvaluatorUUID       string    `db:"evaluator_uuid"`
	EvaluatorExternalID string    `db:"evaluator_external_id"`
	VersionUUID         string    `db:"version_uuid"`
	VersionExternalID   string    `db:"version_external_id"`
	CreatedAt           time.Time `db:"created_at"`
}

type evaluationExperimentBindingWriteParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	ExperimentUUID      string
	Position            int
	EvaluatorUUID       string
	EvaluatorExternalID string
	VersionUUID         string
	VersionExternalID   string
	CreatedAt           time.Time
}

type EvaluationExperimentBindingMapper interface {
	Insert(ctx context.Context, params evaluationExperimentBindingWriteParams) (evaluationExperimentBindingRow, error)
	ListByExperiment(ctx context.Context, organizationUUID, workspaceUUID, experimentUUID string) ([]evaluationExperimentBindingRow, error)
}
