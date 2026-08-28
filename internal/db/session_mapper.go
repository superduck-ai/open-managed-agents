package db

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionMapper -sql ./session_mapper.xml -out ./session_mapper.sqlmap.gen.go -dialect postgres

type sessionMetadataPatchParams struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionExternalID string
	MetadataPatch     []byte
}

type sessionVaultIDs []string

func (ids *sessionVaultIDs) Scan(source any) error {
	if source == nil {
		*ids = sessionVaultIDs{}
		return nil
	}

	var raw []byte
	switch value := source.(type) {
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		return fmt.Errorf("scan session vault_ids: unsupported source type %T", source)
	}

	var decoded []string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("scan session vault_ids: %w", err)
	}
	if decoded == nil {
		decoded = []string{}
	}
	*ids = decoded
	return nil
}

func (ids sessionVaultIDs) Value() (driver.Value, error) {
	if ids == nil {
		return []byte(`[]`), nil
	}
	return json.Marshal([]string(ids))
}

type sessionRow struct {
	UUID                  string          `db:"uuid"`
	ExternalID            string          `db:"external_id"`
	OrganizationUUID      string          `db:"organization_uuid"`
	WorkspaceUUID         string          `db:"workspace_uuid"`
	CreatedByAPIKeyUUID   string          `db:"created_by_api_key_uuid"`
	EnvironmentUUID       string          `db:"environment_uuid"`
	EnvironmentExternalID string          `db:"environment_external_id"`
	AgentUUID             string          `db:"agent_uuid"`
	AgentExternalID       string          `db:"agent_external_id"`
	AgentVersion          int             `db:"agent_version"`
	AgentSnapshot         []byte          `db:"agent_snapshot"`
	DeploymentUUID        *string         `db:"deployment_uuid"`
	DeploymentID          *string         `db:"deployment_external_id"`
	Title                 *string         `db:"title"`
	Metadata              []byte          `db:"metadata"`
	VaultIDs              sessionVaultIDs `db:"vault_ids"`
	Status                string          `db:"status"`
	Usage                 []byte          `db:"usage"`
	Stats                 []byte          `db:"stats"`
	OutcomeEvaluations    []byte          `db:"outcome_evaluations"`
	Budget                []byte          `db:"budget"`
	CreatedAt             time.Time       `db:"created_at"`
	UpdatedAt             time.Time       `db:"updated_at"`
	ArchivedAt            *time.Time      `db:"archived_at"`
	DeletedAt             *time.Time      `db:"deleted_at"`
}

type sessionWriteParams struct {
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
	DeploymentUUID        *string
	DeploymentID          *string
	Title                 *string
	Metadata              []byte
	VaultIDs              sessionVaultIDs
	Status                string
	Usage                 []byte
	Stats                 []byte
	OutcomeEvaluations    []byte
	Budget                []byte
	CreatedAt             time.Time
}

type sessionUpdateParams struct {
	WorkspaceUUID string
	ExternalID    string
	AgentSnapshot []byte
	Title         *string
	Metadata      []byte
	Budget        []byte
	UpdatedAt     time.Time
}

type sessionPageMapperParams struct {
	WorkspaceUUID   string
	FetchLimit      int
	Cursor          *SessionPageCursor
	Descending      bool
	IncludeArchived bool
	AgentExternalID string
	AgentVersion    *int
	DeploymentID    string
	MemoryStoreID   string
	Statuses        []string
	CreatedAtGT     *time.Time
	CreatedAtGTE    *time.Time
	CreatedAtLT     *time.Time
	CreatedAtLTE    *time.Time
}

// SessionMapper contains queries whose primary table is sessions.
type SessionMapper interface {
	Insert(ctx context.Context, params sessionWriteParams) (sessionRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, sessionExternalID string) (sessionRow, bool, error)
	FindByUUID(ctx context.Context, workspaceUUID, sessionUUID string) (sessionRow, bool, error)
	UpdateByExternalID(ctx context.Context, params sessionUpdateParams) (sessionRow, error)
	PatchMetadata(ctx context.Context, workspaceUUID, sessionExternalID string, metadataPatch []byte) (sessionRow, error)
	SetOutcomeEvaluations(ctx context.Context, workspaceUUID, sessionExternalID string, evaluations []byte) (sessionRow, error)
	SetStatus(ctx context.Context, workspaceUUID, sessionExternalID, status string) (int64, error)
	Archive(ctx context.Context, workspaceUUID, sessionExternalID string) (sessionRow, error)
	SoftDelete(ctx context.Context, workspaceUUID, sessionExternalID string) (sessionRow, error)
	ListPage(ctx context.Context, params sessionPageMapperParams) ([]sessionRow, error)
	LockForResourceMutation(ctx context.Context, workspaceUUID, sessionExternalID string) (sessionRow, error)
	LockSessionForEvents(
		ctx context.Context,
		workspaceUUID string,
		sessionExternalID string,
	) (sessionRow, bool, error)

	MergeMetadata(ctx context.Context, params sessionMetadataPatchParams) (int64, error)
}
