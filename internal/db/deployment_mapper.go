package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DeploymentMapper -sql ./deployment_mapper.xml -out ./deployment_mapper.sqlmap.gen.go -dialect postgres

type deploymentMapperRow struct {
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID   string     `db:"created_by_api_key_uuid"`
	EnvironmentUUID       string     `db:"environment_uuid"`
	EnvironmentExternalID string     `db:"environment_external_id"`
	AgentUUID             string     `db:"agent_uuid"`
	AgentExternalID       string     `db:"agent_external_id"`
	AgentVersion          int        `db:"agent_version"`
	AgentSnapshot         []byte     `db:"agent_snapshot"`
	Name                  string     `db:"name"`
	Description           *string    `db:"description"`
	Metadata              []byte     `db:"metadata"`
	InitialEvents         []byte     `db:"initial_events"`
	Resources             []byte     `db:"resources"`
	ResourceSecrets       []byte     `db:"resource_secrets"`
	VaultIDs              []byte     `db:"vault_ids"`
	Schedule              []byte     `db:"schedule"`
	LastRunAt             *time.Time `db:"last_run_at"`
	Status                string     `db:"status"`
	PausedReason          []byte     `db:"paused_reason"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	ArchivedAt            *time.Time `db:"archived_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

type deploymentWriteParams struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CreatedByAPIKeyUUID   string
	EnvironmentUUID       string
	EnvironmentExternalID string
	AgentUUID             string
	AgentExternalID       string
	AgentVersion          int
	AgentSnapshot         []byte
	Name                  string
	Description           *string
	Metadata              []byte
	InitialEvents         []byte
	Resources             []byte
	ResourceSecrets       []byte
	VaultIDs              []byte
	Schedule              []byte
	ScheduleChanged       bool
	LastRunAt             *time.Time
	Status                string
	PausedReason          []byte
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type pauseScheduledDeploymentParams struct {
	WorkspaceUUID string
	ExternalID    string
	PausedReason  []byte
	LastRunAt     time.Time
}

type deploymentPageMapperParams struct {
	WorkspaceUUID   string
	FetchLimit      int
	Cursor          *DeploymentPageCursor
	IncludeArchived bool
	AgentExternalID string
	Status          string
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type DeploymentMapper interface {
	Insert(ctx context.Context, params deploymentWriteParams) (deploymentMapperRow, error)
	CountScheduledByOrganization(ctx context.Context, organizationUUID string) (int64, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (deploymentMapperRow, error)
	LockByExternalID(ctx context.Context, workspaceUUID, externalID string) (deploymentMapperRow, error)
	UpdateByExternalID(ctx context.Context, params deploymentWriteParams) (deploymentMapperRow, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, externalID string) (deploymentMapperRow, error)
	ArchiveByRootAgent(ctx context.Context, workspaceUUID, agentExternalID string) error
	PauseByExternalID(ctx context.Context, workspaceUUID, externalID string, pausedReason []byte) (deploymentMapperRow, error)
	UnpauseByExternalID(ctx context.Context, workspaceUUID, externalID string) (deploymentMapperRow, error)
	ListActiveSchedules(ctx context.Context) ([]DeploymentSchedule, error)
	PauseAfterScheduledRun(ctx context.Context, params pauseScheduledDeploymentParams) (int64, error)
	ListPage(ctx context.Context, params deploymentPageMapperParams) ([]deploymentMapperRow, error)
	UpdateLastRun(ctx context.Context, workspaceUUID, externalID string, lastRunAt time.Time) (int64, error)
}
