package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper VaultMapper -sql ./vault_mapper.xml -out ./vault_mapper.sqlmap.gen.go -dialect postgres

type vaultRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	DisplayName         string     `db:"display_name"`
	Metadata            []byte     `db:"metadata"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type insertVaultParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	DisplayName         string
	Metadata            []byte
	CreatedAt           time.Time
}

type updateVaultParams struct {
	WorkspaceUUID string
	ExternalID    string
	DisplayName   string
	Metadata      []byte
	UpdatedAt     time.Time
}

type listVaultsMapperParams struct {
	WorkspaceUUID   string
	Limit           int
	Cursor          *VaultPageCursor
	IncludeArchived bool
}

type VaultMapper interface {
	Insert(ctx context.Context, params insertVaultParams) (vaultRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (vaultRow, error)
	FindByIdentifier(ctx context.Context, workspaceUUID, identifier, vaultUUID string) (vaultRow, error)
	UpdateByExternalID(ctx context.Context, params updateVaultParams) (vaultRow, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, externalID string) (vaultRow, error)
	FindUUIDForUpdate(ctx context.Context, workspaceUUID, externalID string) (string, error)
	FindActiveUUIDForUpdate(ctx context.Context, workspaceUUID, externalID string) (string, error)
	DeleteByUUID(ctx context.Context, workspaceUUID, vaultUUID string) error
	ListPage(ctx context.Context, params listVaultsMapperParams) ([]vaultRow, error)
}
