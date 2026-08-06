package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
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
	// SecretPayload is transient plaintext for Seal/Open only; never persist.
	SecretPayload json.RawMessage
	// SecretEnvelope is the at-rest sealed secret; required for active writes.
	SecretEnvelope *secrets.Envelope
	SecretVersion  int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
	DeletedAt      *time.Time
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
	mapper := NewVaultMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, vaultInsertParams(vault))
	if err != nil {
		return Vault{}, err
	}
	return row.vault(), nil
}

func (d *DB) GetVault(ctx context.Context, workspaceUUID, externalID string) (Vault, error) {
	mapper := NewVaultMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return Vault{}, mapNoRows(err)
	}
	return row.vault(), nil
}

func (d *DB) GetVaultByExternalIDOrUUID(ctx context.Context, workspaceUUID, identifier string) (Vault, error) {
	mapper := NewVaultMapper(d.mapperDB)
	row, err := mapper.FindByIdentifier(ctx, workspaceUUID, identifier, tryParseDBUUIDIdentifierString(identifier))
	if err != nil {
		return Vault{}, mapNoRows(err)
	}
	return row.vault(), nil
}

func (d *DB) UpdateVault(ctx context.Context, workspaceUUID, externalID string, next Vault) (Vault, error) {
	mapper := NewVaultMapper(d.mapperDB)
	row, err := mapper.UpdateByExternalID(ctx, updateVaultParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    externalID,
		DisplayName:   next.DisplayName,
		Metadata:      next.Metadata,
		UpdatedAt:     next.UpdatedAt,
	})
	if err != nil {
		return Vault{}, mapNoRows(err)
	}
	return row.vault(), nil
}

func (d *DB) ArchiveVault(ctx context.Context, workspaceUUID, externalID string) (Vault, error) {
	var archived Vault
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		vaultMapper := NewVaultMapper(executor)
		credentialMapper := NewVaultCredentialMapper(executor)
		row, err := vaultMapper.ArchiveByExternalID(ctx, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		if err := credentialMapper.ArchiveByVaultUUID(ctx, workspaceUUID, row.UUID); err != nil {
			return err
		}
		archived = row.vault()
		return nil
	})
	return archived, err
}

func (d *DB) DeleteVault(ctx context.Context, workspaceUUID, externalID string) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		vaultMapper := NewVaultMapper(executor)
		credentialMapper := NewVaultCredentialMapper(executor)
		vaultUUID, err := vaultMapper.FindUUIDForUpdate(ctx, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		if err := credentialMapper.DeleteByVaultUUID(ctx, workspaceUUID, vaultUUID); err != nil {
			return err
		}
		return vaultMapper.DeleteByUUID(ctx, workspaceUUID, vaultUUID)
	})
}

func (d *DB) ListVaultsPage(ctx context.Context, params ListVaultsPageParams) ([]Vault, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	mapper := NewVaultMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, listVaultsMapperParams{
		WorkspaceUUID:   params.WorkspaceUUID,
		Limit:           params.Limit + 1,
		Cursor:          params.Cursor,
		IncludeArchived: params.IncludeArchived,
	})
	if err != nil {
		return nil, false, err
	}
	vaults := vaultsFromRows(rows)
	return trimAdminPage(vaults, params.Limit), len(vaults) > params.Limit, nil
}

func (d *DB) CreateVaultCredential(ctx context.Context, credential VaultCredential) (VaultCredential, error) {
	var created VaultCredential
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		vaultMapper := NewVaultMapper(executor)
		credentialMapper := NewVaultCredentialMapper(executor)
		vaultUUID, err := vaultMapper.FindActiveUUIDForUpdate(ctx, credential.WorkspaceUUID, credential.VaultExternalID)
		if err != nil {
			return mapNoRows(err)
		}
		activeCount, err := credentialMapper.CountActive(ctx, credential.WorkspaceUUID, vaultUUID)
		if err != nil {
			return err
		}
		if activeCount >= 20 {
			return ErrLimitExceeded
		}
		credential.VaultUUID = vaultUUID
		params, paramsErr := vaultCredentialInsertParams(credential)
		if paramsErr != nil {
			return paramsErr
		}
		row, err := credentialMapper.Insert(ctx, params)
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		if err != nil {
			return err
		}
		created = row.credential()
		return nil
	})
	return created, err
}

func (d *DB) GetVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (VaultCredential, error) {
	mapper := NewVaultCredentialMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, vaultExternalID, credentialExternalID)
	if err != nil {
		return VaultCredential{}, mapNoRows(err)
	}
	return row.credential(), nil
}

func (d *DB) UpdateVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string, next VaultCredential) (VaultCredential, error) {
	if err := requireCompleteSecretEnvelope(next.SecretEnvelope); err != nil {
		return VaultCredential{}, err
	}
	mapper := NewVaultCredentialMapper(d.mapperDB)
	row, err := mapper.UpdateByExternalID(ctx, vaultCredentialUpdateParams(workspaceUUID, vaultExternalID, credentialExternalID, next))
	if err == nil {
		return row.credential(), nil
	}
	if isUniqueViolation(err) {
		return VaultCredential{}, ErrDuplicate
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return VaultCredential{}, err
	}
	current, getErr := mapper.FindByExternalID(ctx, workspaceUUID, vaultExternalID, credentialExternalID)
	if getErr != nil {
		return VaultCredential{}, mapNoRows(getErr)
	}
	if current.ArchivedAt != nil {
		return VaultCredential{}, ErrNotFound
	}
	if current.Version != next.SecretVersion {
		return VaultCredential{}, ErrVersionConflict
	}
	return VaultCredential{}, ErrNotFound
}

func (d *DB) ArchiveVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (VaultCredential, error) {
	mapper := NewVaultCredentialMapper(d.mapperDB)
	row, err := mapper.ArchiveByExternalID(ctx, workspaceUUID, vaultExternalID, credentialExternalID)
	if err != nil {
		return VaultCredential{}, mapNoRows(err)
	}
	return row.credential(), nil
}

func (d *DB) DeleteVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) error {
	mapper := NewVaultCredentialMapper(d.mapperDB)
	rowsAffected, err := mapper.DeleteByExternalID(ctx, workspaceUUID, vaultExternalID, credentialExternalID)
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
	mapper := NewVaultCredentialMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, listVaultCredentialsMapperParams{
		WorkspaceUUID:   params.WorkspaceUUID,
		VaultExternalID: params.VaultExternalID,
		Limit:           params.Limit + 1,
		Cursor:          params.Cursor,
		IncludeArchived: params.IncludeArchived,
	})
	if err != nil {
		return nil, false, err
	}
	credentials := vaultCredentialsFromRows(rows)
	return trimAdminPage(credentials, params.Limit), len(credentials) > params.Limit, nil
}

// ListActiveVaultCredentialsForVaultIDs returns active credentials in vault_ids
// order. Missing or archived vaults contribute nothing. Per-vault Get+List with
// a 100-credential cap; replace with batch SQL if vault_ids lists grow large.
func (d *DB) ListActiveVaultCredentialsForVaultIDs(ctx context.Context, workspaceUUID string, vaultIDs []string) ([]VaultCredential, error) {
	out := make([]VaultCredential, 0)
	for _, vaultID := range vaultIDs {
		vaultID = strings.TrimSpace(vaultID)
		if vaultID == "" {
			continue
		}
		vault, err := d.GetVault(ctx, workspaceUUID, vaultID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		if vault.ArchivedAt != nil {
			continue
		}
		credentials, _, err := d.ListVaultCredentialsPage(ctx, ListVaultCredentialsPageParams{
			WorkspaceUUID:   workspaceUUID,
			VaultExternalID: vaultID,
			Limit:           100,
			IncludeArchived: false,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, credentials...)
	}
	return out, nil
}

func vaultInsertParams(vault Vault) insertVaultParams {
	return insertVaultParams{
		UUID:                vault.UUID,
		ExternalID:          vault.ExternalID,
		OrganizationUUID:    vault.OrganizationUUID,
		WorkspaceUUID:       vault.WorkspaceUUID,
		CreatedByAPIKeyUUID: vault.CreatedByAPIKeyUUID,
		DisplayName:         vault.DisplayName,
		Metadata:            vault.Metadata,
		CreatedAt:           vault.CreatedAt,
	}
}

func vaultCredentialInsertParams(credential VaultCredential) (insertVaultCredentialParams, error) {
	if err := requireCompleteSecretEnvelope(credential.SecretEnvelope); err != nil {
		return insertVaultCredentialParams{}, err
	}
	ciphertext, nonce, wrappedDEK, formatVersion, keyProvider, keyVersion := vaultCredentialSecretColumns(credential.SecretEnvelope)
	return insertVaultCredentialParams{
		UUID:                credential.UUID,
		ExternalID:          credential.ExternalID,
		OrganizationUUID:    credential.OrganizationUUID,
		WorkspaceUUID:       credential.WorkspaceUUID,
		VaultUUID:           credential.VaultUUID,
		VaultExternalID:     credential.VaultExternalID,
		CreatedByAPIKeyUUID: optionalVaultString(credential.CreatedByAPIKeyUUID),
		DisplayName:         credential.DisplayName,
		Metadata:            credential.Metadata,
		AuthType:            credential.AuthType,
		CredentialKey:       credential.CredentialKey,
		Auth:                credential.Auth,
		Ciphertext:          ciphertext,
		Nonce:               nonce,
		WrappedDEK:          wrappedDEK,
		FormatVersion:       formatVersion,
		KeyProvider:         keyProvider,
		KeyVersion:          keyVersion,
		Version:             credential.SecretVersion,
		CreatedAt:           credential.CreatedAt,
	}, nil
}

func vaultCredentialUpdateParams(
	workspaceUUID, vaultExternalID, credentialExternalID string,
	credential VaultCredential,
) updateVaultCredentialParams {
	ciphertext, nonce, wrappedDEK, formatVersion, keyProvider, keyVersion := vaultCredentialSecretColumns(credential.SecretEnvelope)
	return updateVaultCredentialParams{
		WorkspaceUUID:        workspaceUUID,
		VaultExternalID:      vaultExternalID,
		CredentialExternalID: credentialExternalID,
		DisplayName:          credential.DisplayName,
		Metadata:             credential.Metadata,
		CredentialKey:        credential.CredentialKey,
		Auth:                 credential.Auth,
		Ciphertext:           ciphertext,
		Nonce:                nonce,
		WrappedDEK:           wrappedDEK,
		FormatVersion:        formatVersion,
		KeyProvider:          keyProvider,
		KeyVersion:           keyVersion,
		ExpectedVersion:      credential.SecretVersion,
		UpdatedAt:            credential.UpdatedAt,
	}
}

func optionalVaultString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func vaultsFromRows(rows []vaultRow) []Vault {
	vaults := make([]Vault, len(rows))
	for index := range rows {
		vaults[index] = rows[index].vault()
	}
	return vaults
}

func vaultCredentialsFromRows(rows []vaultCredentialRow) []VaultCredential {
	credentials := make([]VaultCredential, len(rows))
	for index := range rows {
		credentials[index] = rows[index].credential()
	}
	return credentials
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
	createdByAPIKeyUUID := ""
	if r.CreatedByAPIKeyUUID.Valid {
		createdByAPIKeyUUID = r.CreatedByAPIKeyUUID.String
	}
	credential := VaultCredential{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		VaultUUID:           r.VaultUUID,
		VaultExternalID:     r.VaultExternalID,
		CreatedByAPIKeyUUID: createdByAPIKeyUUID,
		DisplayName:         r.DisplayName,
		Metadata:            copyRaw(r.Metadata),
		AuthType:            r.AuthType,
		CredentialKey:       r.CredentialKey,
		Auth:                copyRaw(r.Auth),
		SecretVersion:       r.Version,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		ArchivedAt:          r.ArchivedAt,
		DeletedAt:           r.DeletedAt,
	}
	if len(r.Ciphertext) > 0 {
		credential.SecretEnvelope = &secrets.Envelope{
			Ciphertext:    append([]byte(nil), r.Ciphertext...),
			Nonce:         append([]byte(nil), r.Nonce...),
			WrappedDEK:    append([]byte(nil), r.WrappedDEK...),
			FormatVersion: int(r.FormatVersion.Int32),
			KeyProvider:   r.KeyProvider.String,
			KeyVersion:    r.KeyVersion.Int64,
		}
	}
	return credential
}
