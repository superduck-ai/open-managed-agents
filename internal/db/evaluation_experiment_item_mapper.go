package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationExperimentItemMapper -sql ./evaluation_experiment_item_mapper.xml -out ./evaluation_experiment_item_mapper.sqlmap.gen.go -dialect postgres

type evaluationExperimentItemRow struct {
	UUID              string    `db:"uuid"`
	ExternalID        string    `db:"external_id"`
	OrganizationUUID  string    `db:"organization_uuid"`
	WorkspaceUUID     string    `db:"workspace_uuid"`
	ExperimentUUID    string    `db:"experiment_uuid"`
	CaseUUID          string    `db:"case_uuid"`
	CaseExternalID    string    `db:"case_external_id"`
	Position          int       `db:"position"`
	SessionExternalID *string   `db:"session_external_id"`
	Status            string    `db:"status"`
	ErrorMessage      string    `db:"error_message"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

type evaluationExperimentItemWriteParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	ExperimentUUID   string
	CaseUUID         string
	CaseExternalID   string
	Position         int
	Status           string
	CreatedAt        time.Time
}

type evaluationExperimentItemSessionParams struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	ExternalID        string
	SessionExternalID *string
	Status            string
	ErrorMessage      string
	UpdatedAt         time.Time
}

type evaluationExperimentItemFinishParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ExternalID       string
	Status           string
	ErrorMessage     string
	UpdatedAt        time.Time
}

type evaluationExperimentSessionItemRow struct {
	UUID                 string    `db:"uuid"`
	ExternalID           string    `db:"external_id"`
	OrganizationUUID     string    `db:"organization_uuid"`
	WorkspaceUUID        string    `db:"workspace_uuid"`
	ExperimentUUID       string    `db:"experiment_uuid"`
	CaseUUID             string    `db:"case_uuid"`
	CaseExternalID       string    `db:"case_external_id"`
	Position             int       `db:"position"`
	SessionExternalID    *string   `db:"session_external_id"`
	Status               string    `db:"status"`
	ErrorMessage         string    `db:"error_message"`
	CreatedAt            time.Time `db:"created_at"`
	UpdatedAt            time.Time `db:"updated_at"`
	ExperimentExternalID string    `db:"experiment_external_id"`
}

type EvaluationExperimentItemMapper interface {
	Insert(ctx context.Context, params evaluationExperimentItemWriteParams) (evaluationExperimentItemRow, error)
	ListByExperiment(ctx context.Context, organizationUUID, workspaceUUID, experimentUUID string) ([]evaluationExperimentItemRow, error)
	ListRunningBySession(ctx context.Context, organizationUUID, workspaceUUID, sessionExternalID string) ([]evaluationExperimentSessionItemRow, error)
	SetSession(ctx context.Context, params evaluationExperimentItemSessionParams) (int64, error)
	FinishRunning(ctx context.Context, params evaluationExperimentItemFinishParams) (int64, error)
}
