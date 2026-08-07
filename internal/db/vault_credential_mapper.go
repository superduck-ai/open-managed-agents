package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper VaultCredentialMapper -sql ./vault_credential_mapper.xml -out ./vault_credential_mapper.sqlmap.gen.go -dialect postgres

type vaultCredentialRow struct {
	UUID                string         `db:"uuid"`
	ExternalID          string         `db:"external_id"`
	OrganizationUUID    string         `db:"organization_uuid"`
	WorkspaceUUID       string         `db:"workspace_uuid"`
	VaultUUID           string         `db:"vault_uuid"`
	VaultExternalID     string         `db:"vault_external_id"`
	CreatedByAPIKeyUUID sql.NullString `db:"created_by_api_key_uuid"`
	DisplayName         string         `db:"display_name"`
	Metadata            []byte         `db:"metadata"`
	AuthType            string         `db:"auth_type"`
	CredentialKey       string         `db:"credential_key"`
	Auth                []byte         `db:"auth"`
	SecretPayload       []byte         `db:"secret_payload"`
	CreatedAt           time.Time      `db:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at"`
	ArchivedAt          *time.Time     `db:"archived_at"`
	DeletedAt           *time.Time     `db:"deleted_at"`
}

type insertVaultCredentialParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	VaultUUID           string
	VaultExternalID     string
	CreatedByAPIKeyUUID *string
	DisplayName         string
	Metadata            []byte
	AuthType            string
	CredentialKey       string
	Auth                []byte
	SecretPayload       []byte
	CreatedAt           time.Time
}

type updateVaultCredentialParams struct {
	WorkspaceUUID        string
	VaultExternalID      string
	CredentialExternalID string
	DisplayName          string
	Metadata             []byte
	Auth                 []byte
	SecretPayload        []byte
	UpdatedAt            time.Time
}

type listVaultCredentialsMapperParams struct {
	WorkspaceUUID   string
	VaultExternalID string
	Limit           int
	Cursor          *VaultCredentialPageCursor
	IncludeArchived bool
}

type VaultCredentialMapper interface {
	ArchiveByVaultUUID(ctx context.Context, workspaceUUID, vaultUUID string) error
	DeleteByVaultUUID(ctx context.Context, workspaceUUID, vaultUUID string) error
	CountActive(ctx context.Context, workspaceUUID, vaultUUID string) (int, error)
	Insert(ctx context.Context, params insertVaultCredentialParams) (vaultCredentialRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (vaultCredentialRow, error)
	UpdateByExternalID(ctx context.Context, params updateVaultCredentialParams) (vaultCredentialRow, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (vaultCredentialRow, error)
	DeleteByExternalID(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (int64, error)
	ListPage(ctx context.Context, params listVaultCredentialsMapperParams) ([]vaultCredentialRow, error)
}
