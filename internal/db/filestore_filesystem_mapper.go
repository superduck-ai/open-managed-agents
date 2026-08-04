package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper FilestoreFilesystemMapper -sql ./filestore_filesystem_mapper.xml -out ./filestore_filesystem_mapper.sqlmap.gen.go -dialect postgres

type FilestoreFilesystemMapper interface {
	LockProvision(ctx context.Context, workspaceUUID, filesystemExternalID string) error
	ValidateSessionBinding(ctx context.Context, params filestoreFilesystemProvisionParams) (filestoreSessionBindingRow, bool, error)
	LockWorkspace(ctx context.Context, workspaceUUID string) error
	FindProvisionedByIdentifier(ctx context.Context, workspaceUUID, filesystemExternalID string, filesystemUUID uuid.NullUUID) (filestoreFilesystemRow, bool, error)
	FindProvisionedBySession(ctx context.Context, workspaceUUID, sessionUUID string) (filestoreFilesystemRow, bool, error)
	InsertProvisioned(ctx context.Context, params filestoreFilesystemProvisionParams) (filestoreFilesystemRow, error)
	FindTokenScope(ctx context.Context, organizationUUID, accountUUID, workspaceUUID, workspaceTaggedID, resolvedWorkspaceTaggedID, filesystemID string, filesystemUUID uuid.NullUUID) (filestoreTokenScopeRow, bool, error)
	FindSessionTokenScope(ctx context.Context, workspaceUUID, sessionExternalID string) (filestoreTokenScopeRow, bool, error)
	FindFilesystemByIdentifier(ctx context.Context, workspaceUUID, filesystemID string, filesystemUUID uuid.NullUUID) (filestoreFilesystemRow, bool, error)
	FindFilesystemBySessionExternalID(ctx context.Context, workspaceUUID, sessionExternalID string) (filestoreFilesystemRow, bool, error)
	LockFilesystem(ctx context.Context, filesystemUUID string) error
	FindFilesystemByUUID(ctx context.Context, workspaceUUID, filesystemUUID string) (filestoreFilesystemRow, bool, error)
	FindFilesystemForMutation(ctx context.Context, workspaceUUID, filesystemUUID string) (filestoreFilesystemRow, bool, error)
	FindSessionFilesystemForMutation(ctx context.Context, workspaceUUID, sessionUUID string) (filestoreFilesystemRow, bool, error)
	FindSessionFilesystemByExternalID(ctx context.Context, workspaceUUID, sessionExternalID string) (filestoreFilesystemRow, bool, error)
	RetireSessionFilesystem(ctx context.Context, workspaceUUID, organizationUUID, sessionUUID string, retiredAt time.Time) (filestoreFilesystemRow, bool, error)
	InsertSessionFilesystem(ctx context.Context, params sessionFilesystemInsertParams) (filestoreFilesystemRow, bool, error)
	SessionFilesystemExternalIDExists(ctx context.Context, workspaceUUID, filesystemExternalID string) (bool, error)
}

type filestoreFilesystemProvisionParams struct {
	FilesystemUUID       *string
	FilesystemExternalID string
	OrganizationUUID     string
	WorkspaceUUID        string
	SessionUUID          string
	CodeSessionUUID      *string
	CreatedByAPIKeyUUID  *string
	HasCodeSession       bool
	HasCreatedByAPIKey   bool
	Now                  time.Time
}

type sessionFilesystemInsertParams struct {
	FilesystemExternalID string
	SessionUUID          string
	OrganizationUUID     string
	WorkspaceUUID        string
	CreatedByAPIKeyUUID  string
	CreatedAt            time.Time
}

type filestoreSessionBindingRow struct {
	WorkspaceUUID uuid.UUID `db:"workspace_uuid"`
}

type filestoreFilesystemRow struct {
	UUID                uuid.UUID     `db:"uuid"`
	ExternalID          string        `db:"external_id"`
	OrganizationUUID    uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID       uuid.UUID     `db:"workspace_uuid"`
	SessionUUID         uuid.UUID     `db:"session_uuid"`
	CodeSessionUUID     uuid.NullUUID `db:"code_session_uuid"`
	CreatedByAPIKeyUUID uuid.NullUUID `db:"created_by_api_key_uuid"`
	CreatedAt           time.Time     `db:"created_at"`
	UpdatedAt           time.Time     `db:"updated_at"`
	DeletedAt           *time.Time    `db:"deleted_at"`
}

type filestoreTokenScopeRow struct {
	OrganizationUUID     uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID        uuid.UUID `db:"workspace_uuid"`
	WorkspaceExternalID  string    `db:"workspace_external_id"`
	AccountUUID          uuid.UUID `db:"account_uuid"`
	AccountExternalID    string    `db:"account_external_id"`
	FilesystemUUID       uuid.UUID `db:"filesystem_uuid"`
	FilesystemExternalID string    `db:"filesystem_external_id"`
	OrgTaintsJSON        []byte    `db:"org_taints_json"`
	WorkspaceCMEKEnabled bool      `db:"workspace_cmek_enabled"`
}

func filestoreFilesystemFromMapperRow(
	row filestoreFilesystemRow,
	found bool,
	err error,
) (FilestoreFilesystem, error) {
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if !found {
		return FilestoreFilesystem{}, ErrNotFound
	}
	return row.filesystem()
}

func filestoreTokenScopeFromMapperRow(
	row filestoreTokenScopeRow,
	found bool,
	err error,
) (FilestoreTokenScope, error) {
	if err != nil {
		return FilestoreTokenScope{}, err
	}
	if !found {
		return FilestoreTokenScope{}, ErrNotFound
	}
	return row.scope()
}

func (row filestoreFilesystemRow) filesystem() (FilestoreFilesystem, error) {
	if row.SessionUUID == uuid.Nil {
		return FilestoreFilesystem{}, ErrNotFound
	}
	return FilestoreFilesystem{
		UUID:                row.UUID.String(),
		ExternalID:          row.ExternalID,
		OrganizationUUID:    row.OrganizationUUID.String(),
		WorkspaceUUID:       row.WorkspaceUUID.String(),
		SessionUUID:         row.SessionUUID.String(),
		CodeSessionUUID:     nullableUUIDString(row.CodeSessionUUID),
		CreatedByAPIKeyUUID: nullableUUIDString(row.CreatedByAPIKeyUUID),
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		DeletedAt:           row.DeletedAt,
	}, nil
}

func (row filestoreTokenScopeRow) scope() (FilestoreTokenScope, error) {
	var orgTaints []string
	if err := json.Unmarshal(row.OrgTaintsJSON, &orgTaints); err != nil {
		return FilestoreTokenScope{}, fmt.Errorf("decode Filestore organization taints: %w", err)
	}
	if orgTaints == nil {
		orgTaints = []string{}
	}
	return FilestoreTokenScope{
		OrganizationUUID:     row.OrganizationUUID.String(),
		WorkspaceUUID:        row.WorkspaceUUID.String(),
		WorkspaceExternalID:  row.WorkspaceExternalID,
		AccountUUID:          row.AccountUUID.String(),
		AccountExternalID:    row.AccountExternalID,
		FilesystemUUID:       row.FilesystemUUID.String(),
		FilesystemExternalID: row.FilesystemExternalID,
		OrgTaints:            orgTaints,
		WorkspaceCMEKEnabled: row.WorkspaceCMEKEnabled,
	}, nil
}
