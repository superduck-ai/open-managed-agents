package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Vault struct {
	ID                int64
	UUID              string
	ExternalID        string
	OrganizationID    int64
	WorkspaceID       int64
	CreatedByAPIKeyID int64
	DisplayName       string
	Metadata          json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	DeletedAt         *time.Time
}

type VaultPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type ListVaultsPageParams struct {
	WorkspaceID     int64
	Limit           int
	Cursor          *VaultPageCursor
	IncludeArchived bool
}

type VaultCredential struct {
	ID                int64
	UUID              string
	ExternalID        string
	OrganizationID    int64
	WorkspaceID       int64
	VaultID           int64
	VaultExternalID   string
	CreatedByAPIKeyID int64
	DisplayName       string
	Metadata          json.RawMessage
	AuthType          string
	CredentialKey     string
	Auth              json.RawMessage
	SecretPayload     json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	DeletedAt         *time.Time
}

type VaultCredentialPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type ListVaultCredentialsPageParams struct {
	WorkspaceID     int64
	VaultExternalID string
	Limit           int
	Cursor          *VaultCredentialPageCursor
	IncludeArchived bool
}

func (d *DB) CreateVault(ctx context.Context, vault Vault) (Vault, error) {
	return scanVault(d.Pool.QueryRow(ctx, `
		insert into vaults (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			display_name, metadata, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $8)
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, display_name, metadata, created_at, updated_at,
			archived_at, deleted_at
	`, vault.UUID, vault.ExternalID, vault.OrganizationID, vault.WorkspaceID, vault.CreatedByAPIKeyID,
		vault.DisplayName, jsonArg(vault.Metadata), vault.CreatedAt))
}

func (d *DB) GetVault(ctx context.Context, workspaceID int64, externalID string) (Vault, error) {
	return scanVault(d.Pool.QueryRow(ctx, vaultSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) GetVaultByExternalIDOrUUID(ctx context.Context, workspaceID int64, identifier string) (Vault, error) {
	return scanVault(d.Pool.QueryRow(ctx, vaultSelectSQL()+`
		where workspace_id = $1
			and (external_id = $2 or uuid::text = $2)
			and deleted_at is null
	`, workspaceID, identifier))
}

func (d *DB) UpdateVault(ctx context.Context, workspaceID int64, externalID string, next Vault) (Vault, error) {
	return scanVault(d.Pool.QueryRow(ctx, `
		update vaults
		set display_name = $3,
			metadata = $4::jsonb,
			updated_at = $5
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, display_name, metadata, created_at, updated_at,
			archived_at, deleted_at
	`, workspaceID, externalID, next.DisplayName, jsonArg(next.Metadata), next.UpdatedAt))
}

func (d *DB) ArchiveVault(ctx context.Context, workspaceID int64, externalID string) (Vault, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Vault{}, err
	}
	defer tx.Rollback(ctx)

	vault, err := scanVault(tx.QueryRow(ctx, `
		update vaults
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, display_name, metadata, created_at, updated_at,
			archived_at, deleted_at
	`, workspaceID, externalID))
	if err != nil {
		return Vault{}, err
	}
	if _, err := tx.Exec(ctx, `
		update vault_credentials
		set archived_at = coalesce(archived_at, now()),
			secret_payload = null,
			updated_at = now()
		where workspace_id = $1 and vault_id = $2 and deleted_at is null
	`, workspaceID, vault.ID); err != nil {
		return Vault{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Vault{}, err
	}
	return vault, nil
}

func (d *DB) DeleteVault(ctx context.Context, workspaceID int64, externalID string) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var vaultID int64
	if err := tx.QueryRow(ctx, `
		select id
		from vaults
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, externalID).Scan(&vaultID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		delete from vault_credentials
		where workspace_id = $1 and vault_id = $2
	`, workspaceID, vaultID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		delete from vaults
		where workspace_id = $1 and id = $2
	`, workspaceID, vaultID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) ListVaultsPage(ctx context.Context, params ListVaultsPageParams) ([]Vault, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := vaultSelectSQL() + `
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	vaults, err := scanVaultRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(vaults) > params.Limit
	if hasMore {
		vaults = vaults[:params.Limit]
	}
	return vaults, hasMore, nil
}

func (d *DB) CreateVaultCredential(ctx context.Context, credential VaultCredential) (VaultCredential, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return VaultCredential{}, err
	}
	defer tx.Rollback(ctx)

	var vaultID int64
	if err := tx.QueryRow(ctx, `
		select id
		from vaults
		where workspace_id = $1 and external_id = $2 and deleted_at is null and archived_at is null
		for update
	`, credential.WorkspaceID, credential.VaultExternalID).Scan(&vaultID); errors.Is(err, pgx.ErrNoRows) {
		return VaultCredential{}, ErrNotFound
	} else if err != nil {
		return VaultCredential{}, err
	}

	var activeCount int
	if err := tx.QueryRow(ctx, `
		select count(*)::int
		from vault_credentials
		where workspace_id = $1
			and vault_id = $2
			and deleted_at is null
			and archived_at is null
	`, credential.WorkspaceID, vaultID).Scan(&activeCount); err != nil {
		return VaultCredential{}, err
	}
	if activeCount >= 20 {
		return VaultCredential{}, ErrLimitExceeded
	}

	credential.VaultID = vaultID
	created, err := scanVaultCredential(tx.QueryRow(ctx, `
		insert into vault_credentials (
			uuid, external_id, organization_id, workspace_id, vault_id, vault_external_id,
			created_by_api_key_id, display_name, metadata, auth_type, credential_key,
			auth, secret_payload, created_at, updated_at
		)
		values (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9::jsonb, $10, $11,
			$12::jsonb, $13::jsonb, $14, $14
		)
		returning id, uuid::text, external_id, organization_id, workspace_id, vault_id,
			vault_external_id, created_by_api_key_id, display_name, metadata, auth_type,
			credential_key, auth, secret_payload, created_at, updated_at, archived_at,
			deleted_at
	`, credential.UUID, credential.ExternalID, credential.OrganizationID, credential.WorkspaceID,
		credential.VaultID, credential.VaultExternalID, credential.CreatedByAPIKeyID,
		credential.DisplayName, jsonArg(credential.Metadata), credential.AuthType,
		credential.CredentialKey, jsonArg(credential.Auth), jsonArg(credential.SecretPayload),
		credential.CreatedAt))
	if isUniqueViolation(err) {
		return VaultCredential{}, ErrDuplicate
	}
	if err != nil {
		return VaultCredential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return VaultCredential{}, err
	}
	return created, nil
}

func (d *DB) GetVaultCredential(ctx context.Context, workspaceID int64, vaultExternalID, credentialExternalID string) (VaultCredential, error) {
	return scanVaultCredential(d.Pool.QueryRow(ctx, vaultCredentialSelectSQL()+`
		where workspace_id = $1
			and vault_external_id = $2
			and external_id = $3
			and deleted_at is null
	`, workspaceID, vaultExternalID, credentialExternalID))
}

func (d *DB) UpdateVaultCredential(ctx context.Context, workspaceID int64, vaultExternalID, credentialExternalID string, next VaultCredential) (VaultCredential, error) {
	return scanVaultCredential(d.Pool.QueryRow(ctx, `
		update vault_credentials
		set display_name = $4,
			metadata = $5::jsonb,
			auth = $6::jsonb,
			secret_payload = $7::jsonb,
			updated_at = $8
		where workspace_id = $1
			and vault_external_id = $2
			and external_id = $3
			and deleted_at is null
			and archived_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id, vault_id,
			vault_external_id, created_by_api_key_id, display_name, metadata, auth_type,
			credential_key, auth, secret_payload, created_at, updated_at, archived_at,
			deleted_at
	`, workspaceID, vaultExternalID, credentialExternalID, next.DisplayName,
		jsonArg(next.Metadata), jsonArg(next.Auth), jsonArg(next.SecretPayload), next.UpdatedAt))
}

func (d *DB) ArchiveVaultCredential(ctx context.Context, workspaceID int64, vaultExternalID, credentialExternalID string) (VaultCredential, error) {
	return scanVaultCredential(d.Pool.QueryRow(ctx, `
		update vault_credentials
		set archived_at = coalesce(archived_at, now()),
			secret_payload = null,
			updated_at = now()
		where workspace_id = $1
			and vault_external_id = $2
			and external_id = $3
			and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id, vault_id,
			vault_external_id, created_by_api_key_id, display_name, metadata, auth_type,
			credential_key, auth, secret_payload, created_at, updated_at, archived_at,
			deleted_at
	`, workspaceID, vaultExternalID, credentialExternalID))
}

func (d *DB) DeleteVaultCredential(ctx context.Context, workspaceID int64, vaultExternalID, credentialExternalID string) error {
	tag, err := d.Pool.Exec(ctx, `
		delete from vault_credentials
		where workspace_id = $1
			and vault_external_id = $2
			and external_id = $3
			and deleted_at is null
	`, workspaceID, vaultExternalID, credentialExternalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListVaultCredentialsPage(ctx context.Context, params ListVaultCredentialsPageParams) ([]VaultCredential, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := vaultCredentialSelectSQL() + `
		where workspace_id = $1
			and vault_external_id = $2
			and deleted_at is null
	`
	args := []any{params.WorkspaceID, params.VaultExternalID}
	nextArg := 3
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	credentials, err := scanVaultCredentialRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(credentials) > params.Limit
	if hasMore {
		credentials = credentials[:params.Limit]
	}
	return credentials, hasMore, nil
}

func vaultSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, display_name, metadata, created_at, updated_at,
			archived_at, deleted_at
		from vaults
	`
}

func vaultCredentialSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id, vault_id,
			vault_external_id, created_by_api_key_id, display_name, metadata, auth_type,
			credential_key, auth, secret_payload, created_at, updated_at, archived_at,
			deleted_at
		from vault_credentials
	`
}

type vaultScanner interface {
	Scan(dest ...any) error
}

type vaultRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanVault(row vaultScanner) (Vault, error) {
	var vault Vault
	var metadata []byte
	err := row.Scan(&vault.ID, &vault.UUID, &vault.ExternalID, &vault.OrganizationID,
		&vault.WorkspaceID, &vault.CreatedByAPIKeyID, &vault.DisplayName, &metadata,
		&vault.CreatedAt, &vault.UpdatedAt, &vault.ArchivedAt, &vault.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Vault{}, ErrNotFound
	}
	if err != nil {
		return Vault{}, err
	}
	vault.Metadata = copyRaw(metadata)
	return vault, nil
}

func scanVaultRows(rows vaultRows) ([]Vault, error) {
	var vaults []Vault
	for rows.Next() {
		vault, err := scanVault(rows)
		if err != nil {
			return nil, err
		}
		vaults = append(vaults, vault)
	}
	return vaults, rows.Err()
}

func scanVaultCredential(row vaultScanner) (VaultCredential, error) {
	var credential VaultCredential
	var metadata, auth, secretPayload []byte
	err := row.Scan(&credential.ID, &credential.UUID, &credential.ExternalID,
		&credential.OrganizationID, &credential.WorkspaceID, &credential.VaultID,
		&credential.VaultExternalID, &credential.CreatedByAPIKeyID, &credential.DisplayName,
		&metadata, &credential.AuthType, &credential.CredentialKey, &auth, &secretPayload,
		&credential.CreatedAt, &credential.UpdatedAt, &credential.ArchivedAt,
		&credential.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return VaultCredential{}, ErrNotFound
	}
	if err != nil {
		return VaultCredential{}, err
	}
	credential.Metadata = copyRaw(metadata)
	credential.Auth = copyRaw(auth)
	credential.SecretPayload = copyRaw(secretPayload)
	return credential, nil
}

func scanVaultCredentialRows(rows vaultRows) ([]VaultCredential, error) {
	var credentials []VaultCredential
	for rows.Next() {
		credential, err := scanVaultCredential(rows)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, rows.Err()
}
