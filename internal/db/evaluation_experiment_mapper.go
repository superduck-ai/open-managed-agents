package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EvaluationExperimentMapper -sql ./evaluation_experiment_mapper.xml -out ./evaluation_experiment_mapper.sqlmap.gen.go -dialect postgres

type evaluationExperimentRow struct {
	UUID                  string    `db:"uuid"`
	ExternalID            string    `db:"external_id"`
	OrganizationUUID      string    `db:"organization_uuid"`
	WorkspaceUUID         string    `db:"workspace_uuid"`
	Name                  string    `db:"name"`
	Description           string    `db:"description"`
	DatasetUUID           string    `db:"dataset_uuid"`
	DatasetExternalID     string    `db:"dataset_external_id"`
	VersionUUID           string    `db:"version_uuid"`
	VersionExternalID     string    `db:"version_external_id"`
	AgentExternalID       string    `db:"agent_external_id"`
	AgentVersion          int       `db:"agent_version"`
	EnvironmentExternalID string    `db:"environment_external_id"`
	TimeoutSeconds        int       `db:"timeout_seconds"`
	EnvJSON               *string   `db:"env_json"`
	ResourcesJSON         *string   `db:"resources_json"`
	EvaluatorSetKey       string    `db:"evaluator_set_key"`
	Status                string    `db:"status"`
	CreatedByUserUUID     *string   `db:"created_by_user_uuid"`
	CreatedAt             time.Time `db:"created_at"`
	UpdatedAt             time.Time `db:"updated_at"`
	ItemCount             int64     `db:"item_count"`
	SessionCount          int64     `db:"session_count"`
	EvaluatorCount        int64     `db:"evaluator_count"`
}

type evaluationExperimentWriteParams struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	Name                  string
	Description           string
	DatasetUUID           string
	DatasetExternalID     string
	VersionUUID           string
	VersionExternalID     string
	AgentExternalID       string
	AgentVersion          int
	EnvironmentExternalID string
	TimeoutSeconds        int
	EnvJSON               *string
	ResourcesJSON         *string
	EvaluatorSetKey       string
	Status                string
	CreatedByUserUUID     *string
	CreatedAt             time.Time
}

type evaluationExperimentPageParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	FetchLimit       int
	Cursor           *EvaluationExperimentPageCursor
}

type evaluationExperimentStatusParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ExternalID       string
	Status           string
	UpdatedAt        time.Time
}

type evaluationExperimentCompareParams struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	VersionExternalID string
	EvaluatorSetKey   string
	Status            string
}

type EvaluationExperimentMapper interface {
	Insert(ctx context.Context, params evaluationExperimentWriteParams) (evaluationExperimentRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (evaluationExperimentRow, error)
	ListPage(ctx context.Context, params evaluationExperimentPageParams) ([]evaluationExperimentRow, error)
	ListMatching(ctx context.Context, params evaluationExperimentCompareParams) ([]evaluationExperimentRow, error)
	UpdateStatus(ctx context.Context, params evaluationExperimentStatusParams) (int64, error)
}
