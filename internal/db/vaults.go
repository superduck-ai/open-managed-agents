package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Vault struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	DisplayName         string
	Metadata            json.RawMessage
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
	DeletedAt           *time.Time
}

type VaultPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListVaultsPageParams struct {
	WorkspaceUUID   string
	Limit           int
	Cursor          *VaultPageCursor
	IncludeArchived bool
}

type VaultCredential struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	VaultUUID           string
	VaultExternalID     string
	CreatedByAPIKeyUUID string
	DisplayName         string
	Metadata            json.RawMessage
	AuthType            string
	CredentialKey       string
	Auth                json.RawMessage
	SecretPayload       json.RawMessage
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
	DeletedAt           *time.Time
}

type VaultCredentialPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListVaultCredentialsPageParams struct {
	WorkspaceUUID   string
	VaultExternalID string
	Limit           int
	Cursor          *VaultCredentialPageCursor
	IncludeArchived bool
}

func (d *DB) CreateVault(ctx context.Context, vault Vault) (Vault, error) {
	return getVaultSQLX(ctx, d.sql, `
		insert into vaults (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			display_name, metadata, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :created_by_api_key_uuid,
			:display_name, CAST(:metadata AS jsonb), :created_at, :created_at
		)
		returning `+vaultSQLXColumns+`
	`, vaultArguments(vault))
}

func (d *DB) GetVault(ctx context.Context, workspaceUUID, externalID string) (Vault, error) {
	return getVaultSQLX(ctx, d.sql, vaultSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
	`, vaultLookupArguments(workspaceUUID, externalID))
}

func (d *DB) GetVaultByExternalIDOrUUID(ctx context.Context, workspaceUUID, identifier string) (Vault, error) {
	return getVaultSQLX(ctx, d.sql, vaultSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and (external_id = :identifier or CAST(uuid AS text) = :identifier)
			and deleted_at is null
	`, map[string]any{"workspace_uuid": workspaceUUID, "identifier": identifier})
}

func (d *DB) UpdateVault(ctx context.Context, workspaceUUID, externalID string, next Vault) (Vault, error) {
	arguments := vaultArguments(next)
	arguments["workspace_uuid"] = workspaceUUID
	arguments["external_id"] = externalID
	return getVaultSQLX(ctx, d.sql, `
		update vaults
		set display_name = :display_name,
			metadata = CAST(:metadata AS jsonb),
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		returning `+vaultSQLXColumns+`
	`, arguments)
}

func (d *DB) ArchiveVault(ctx context.Context, workspaceUUID, externalID string) (Vault, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Vault{}, err
	}
	defer tx.Rollback()

	vault, err := getVaultSQLX(ctx, tx, `
		update vaults
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		returning `+vaultSQLXColumns+`
	`, vaultLookupArguments(workspaceUUID, externalID))
	if err != nil {
		return Vault{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		update vault_credentials
		set archived_at = coalesce(archived_at, now()),
			secret_payload = null,
			updated_at = now()
		where workspace_uuid = :workspace_uuid and vault_uuid = :vault_uuid and deleted_at is null
	`, map[string]any{"workspace_uuid": workspaceUUID, "vault_uuid": vault.UUID}); err != nil {
		return Vault{}, err
	}
	if err := tx.Commit(); err != nil {
		return Vault{}, err
	}
	return vault, nil
}

func (d *DB) DeleteVault(ctx context.Context, workspaceUUID, externalID string) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var vaultUUID string
	err = namedGetContext(ctx, tx, &vaultUUID, `
		select CAST(uuid AS text)
		from vaults
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, vaultLookupArguments(workspaceUUID, externalID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	arguments := map[string]any{"workspace_uuid": workspaceUUID, "vault_uuid": vaultUUID}
	if _, err := namedExecContext(ctx, tx, `
		delete from vault_credentials
		where workspace_uuid = :workspace_uuid and vault_uuid = :vault_uuid
	`, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `
		delete from vaults
		where workspace_uuid = :workspace_uuid and uuid = CAST(:vault_uuid AS uuid)
	`, arguments); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) ListVaultsPage(ctx context.Context, params ListVaultsPageParams) ([]Vault, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query, arguments := listVaultsQuery(params)
	vaults, err := selectVaultsSQLX(ctx, d.sql, query, arguments)
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
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return VaultCredential{}, err
	}
	defer tx.Rollback()

	var vaultUUID string
	err = namedGetContext(ctx, tx, &vaultUUID, `
		select CAST(uuid AS text)
		from vaults
		where workspace_uuid = :workspace_uuid
			and external_id = :vault_external_id
			and deleted_at is null
			and archived_at is null
		for update
	`, map[string]any{
		"workspace_uuid":    credential.WorkspaceUUID,
		"vault_external_id": credential.VaultExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return VaultCredential{}, ErrNotFound
	}
	if err != nil {
		return VaultCredential{}, err
	}

	var activeCount int
	if err := namedGetContext(ctx, tx, &activeCount, `
		select CAST(count(*) AS int)
		from vault_credentials
		where workspace_uuid = :workspace_uuid
			and vault_uuid = :vault_uuid
			and deleted_at is null
			and archived_at is null
	`, map[string]any{"workspace_uuid": credential.WorkspaceUUID, "vault_uuid": vaultUUID}); err != nil {
		return VaultCredential{}, err
	}
	if activeCount >= 20 {
		return VaultCredential{}, ErrLimitExceeded
	}

	credential.VaultUUID = vaultUUID
	created, err := getVaultCredentialSQLX(ctx, tx, `
		insert into vault_credentials (
			uuid, external_id, organization_uuid, workspace_uuid, vault_uuid, vault_external_id,
			created_by_api_key_uuid, display_name, metadata, auth_type, credential_key,
			auth, secret_payload, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :vault_uuid,
			:vault_external_id, CAST(nullif(:created_by_api_key_uuid, '') AS uuid), :display_name,
			CAST(:metadata AS jsonb), :auth_type, :credential_key,
			CAST(:auth AS jsonb), CAST(:secret_payload AS jsonb),
			:created_at, :created_at
		)
		returning `+vaultCredentialSQLXColumns+`
	`, vaultCredentialArguments(credential))
	if isUniqueViolation(err) {
		return VaultCredential{}, ErrDuplicate
	}
	if err != nil {
		return VaultCredential{}, err
	}
	if err := tx.Commit(); err != nil {
		return VaultCredential{}, err
	}
	return created, nil
}

func (d *DB) GetVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (VaultCredential, error) {
	return getVaultCredentialSQLX(ctx, d.sql, vaultCredentialSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and vault_external_id = :vault_external_id
			and external_id = :credential_external_id
			and deleted_at is null
	`, vaultCredentialLookupArguments(workspaceUUID, vaultExternalID, credentialExternalID))
}

func (d *DB) UpdateVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string, next VaultCredential) (VaultCredential, error) {
	arguments := vaultCredentialArguments(next)
	arguments["workspace_uuid"] = workspaceUUID
	arguments["vault_external_id"] = vaultExternalID
	arguments["credential_external_id"] = credentialExternalID
	return getVaultCredentialSQLX(ctx, d.sql, `
		update vault_credentials
		set display_name = :display_name,
			metadata = CAST(:metadata AS jsonb),
			auth = CAST(:auth AS jsonb),
			secret_payload = CAST(:secret_payload AS jsonb),
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid
			and vault_external_id = :vault_external_id
			and external_id = :credential_external_id
			and deleted_at is null
			and archived_at is null
		returning `+vaultCredentialSQLXColumns+`
	`, arguments)
}

func (d *DB) ArchiveVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (VaultCredential, error) {
	return getVaultCredentialSQLX(ctx, d.sql, `
		update vault_credentials
		set archived_at = coalesce(archived_at, now()),
			secret_payload = null,
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and vault_external_id = :vault_external_id
			and external_id = :credential_external_id
			and deleted_at is null
		returning `+vaultCredentialSQLXColumns+`
	`, vaultCredentialLookupArguments(workspaceUUID, vaultExternalID, credentialExternalID))
}

func (d *DB) DeleteVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		delete from vault_credentials
		where workspace_uuid = :workspace_uuid
			and vault_external_id = :vault_external_id
			and external_id = :credential_external_id
			and deleted_at is null
	`, vaultCredentialLookupArguments(workspaceUUID, vaultExternalID, credentialExternalID))
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListVaultCredentialsPage(ctx context.Context, params ListVaultCredentialsPageParams) ([]VaultCredential, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query, arguments := listVaultCredentialsQuery(params)
	credentials, err := selectVaultCredentialsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(credentials) > params.Limit
	if hasMore {
		credentials = credentials[:params.Limit]
	}
	return credentials, hasMore, nil
}

const (
	vaultSQLXColumns = `CAST(uuid AS text) AS uuid, external_id, CAST(organization_uuid AS text) AS organization_uuid,
		CAST(workspace_uuid AS text) AS workspace_uuid, CAST(created_by_api_key_uuid AS text) AS created_by_api_key_uuid, display_name, metadata, created_at,
		updated_at, archived_at, deleted_at`
	vaultCredentialSQLXColumns = `CAST(uuid AS text) AS uuid, external_id,
		CAST(organization_uuid AS text) AS organization_uuid, CAST(workspace_uuid AS text) AS workspace_uuid,
		CAST(vault_uuid AS text) AS vault_uuid, vault_external_id,
		coalesce(CAST(created_by_api_key_uuid AS text), '') AS created_by_api_key_uuid,
		display_name, metadata, auth_type, credential_key, auth, secret_payload,
		created_at, updated_at, archived_at, deleted_at`
)

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

type vaultCredentialRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	VaultUUID           string     `db:"vault_uuid"`
	VaultExternalID     string     `db:"vault_external_id"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	DisplayName         string     `db:"display_name"`
	Metadata            []byte     `db:"metadata"`
	AuthType            string     `db:"auth_type"`
	CredentialKey       string     `db:"credential_key"`
	Auth                []byte     `db:"auth"`
	SecretPayload       []byte     `db:"secret_payload"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

func vaultSelectSQL() string {
	return `select ` + vaultSQLXColumns + ` from vaults`
}

func vaultCredentialSelectSQL() string {
	return `select ` + vaultCredentialSQLXColumns + ` from vault_credentials`
}

func listVaultsQuery(params ListVaultsPageParams) (string, map[string]any) {
	query := vaultSelectSQL() + ` where workspace_uuid = :workspace_uuid and deleted_at is null`
	arguments := map[string]any{"workspace_uuid": params.WorkspaceUUID, "limit": params.Limit + 1}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < CAST(:cursor_uuid AS uuid)))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = params.Cursor.UUID
	}
	query += " order by created_at desc, uuid desc limit :limit"
	return query, arguments
}

func listVaultCredentialsQuery(params ListVaultCredentialsPageParams) (string, map[string]any) {
	query := vaultCredentialSelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and vault_external_id = :vault_external_id
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid":    params.WorkspaceUUID,
		"vault_external_id": params.VaultExternalID,
		"limit":             params.Limit + 1,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < CAST(:cursor_uuid AS uuid)))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = params.Cursor.UUID
	}
	query += " order by created_at desc, uuid desc limit :limit"
	return query, arguments
}

func getVaultSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (Vault, error) {
	var row vaultRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Vault{}, ErrNotFound
		}
		return Vault{}, err
	}
	return row.vault(), nil
}

func selectVaultsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]Vault, error) {
	var rows []vaultRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	vaults := make([]Vault, len(rows))
	for index := range rows {
		vaults[index] = rows[index].vault()
	}
	return vaults, nil
}

func getVaultCredentialSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (VaultCredential, error) {
	var row vaultCredentialRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VaultCredential{}, ErrNotFound
		}
		return VaultCredential{}, err
	}
	return row.credential(), nil
}

func selectVaultCredentialsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]VaultCredential, error) {
	var rows []vaultCredentialRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	credentials := make([]VaultCredential, len(rows))
	for index := range rows {
		credentials[index] = rows[index].credential()
	}
	return credentials, nil
}

func vaultLookupArguments(workspaceUUID, externalID string) map[string]any {
	return map[string]any{"workspace_uuid": workspaceUUID, "external_id": externalID}
}

func vaultCredentialLookupArguments(
	workspaceUUID string,
	vaultExternalID string,
	credentialExternalID string,
) map[string]any {
	return map[string]any{
		"workspace_uuid":         workspaceUUID,
		"vault_external_id":      vaultExternalID,
		"credential_external_id": credentialExternalID,
	}
}

func vaultArguments(vault Vault) map[string]any {
	return map[string]any{
		"uuid":                    vault.UUID,
		"external_id":             vault.ExternalID,
		"organization_uuid":       vault.OrganizationUUID,
		"workspace_uuid":          vault.WorkspaceUUID,
		"created_by_api_key_uuid": vault.CreatedByAPIKeyUUID,
		"display_name":            vault.DisplayName,
		"metadata":                jsonArg(vault.Metadata),
		"created_at":              vault.CreatedAt,
		"updated_at":              vault.UpdatedAt,
	}
}

func vaultCredentialArguments(credential VaultCredential) map[string]any {
	return map[string]any{
		"uuid":                    credential.UUID,
		"external_id":             credential.ExternalID,
		"organization_uuid":       credential.OrganizationUUID,
		"workspace_uuid":          credential.WorkspaceUUID,
		"vault_uuid":              credential.VaultUUID,
		"vault_external_id":       credential.VaultExternalID,
		"created_by_api_key_uuid": credential.CreatedByAPIKeyUUID,
		"display_name":            credential.DisplayName,
		"metadata":                jsonArg(credential.Metadata),
		"auth_type":               credential.AuthType,
		"credential_key":          credential.CredentialKey,
		"auth":                    jsonArg(credential.Auth),
		"secret_payload":          jsonArg(credential.SecretPayload),
		"created_at":              credential.CreatedAt,
		"updated_at":              credential.UpdatedAt,
	}
}

func (r vaultRow) vault() Vault {
	return Vault{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		DisplayName:         r.DisplayName,
		Metadata:            copyRaw(r.Metadata),
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		ArchivedAt:          r.ArchivedAt,
		DeletedAt:           r.DeletedAt,
	}
}

func (r vaultCredentialRow) credential() VaultCredential {
	return VaultCredential{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		VaultUUID:           r.VaultUUID,
		VaultExternalID:     r.VaultExternalID,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		DisplayName:         r.DisplayName,
		Metadata:            copyRaw(r.Metadata),
		AuthType:            r.AuthType,
		CredentialKey:       r.CredentialKey,
		Auth:                copyRaw(r.Auth),
		SecretPayload:       copyRaw(r.SecretPayload),
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		ArchivedAt:          r.ArchivedAt,
		DeletedAt:           r.DeletedAt,
	}
}
