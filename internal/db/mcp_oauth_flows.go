package db

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type MCPOAuthFlow struct {
	UUID                      string
	ExternalID                string
	OrganizationUUID          string
	WorkspaceUUID             string
	VaultUUID                 string
	VaultExternalID           string
	UserUUID                  string
	UserExternalID            string
	PlatformSessionExternalID string
	MCPServerURL              string
	RedirectURL               string
	DisplayName               string
	Source                    string
	AuthorizationEndpoint     string
	TokenEndpoint             string
	RegistrationEndpoint      string
	Issuer                    string
	Resource                  string
	Scope                     string
	ClientID                  string
	ClientCredentialSource    string
	TokenEndpointAuthMethod   string
	CodeChallengeMethod       string
	// SecretEnvelope holds sealed client_secret (when source=sealed) and code_verifier.
	SecretEnvelope       *secrets.Envelope
	Status               string
	CredentialExternalID string
	ErrorCode            string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ExpiresAt            time.Time
	CompletedAt          *time.Time
}

func (d *DB) CreateMCPOAuthFlow(ctx context.Context, flow MCPOAuthFlow) (MCPOAuthFlow, error) {
	mapper := NewMCPOAuthFlowMapper(d.mapperDB)
	params, err := mcpOAuthFlowInsertParams(flow)
	if err != nil {
		return MCPOAuthFlow{}, err
	}
	row, err := mapper.Insert(ctx, params)
	if err != nil {
		return MCPOAuthFlow{}, err
	}
	return row.flow(), nil
}

func (d *DB) GetMCPOAuthFlow(ctx context.Context, externalID string) (MCPOAuthFlow, error) {
	mapper := NewMCPOAuthFlowMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, externalID)
	if err != nil {
		return MCPOAuthFlow{}, mapNoRows(err)
	}
	return row.flow(), nil
}

func (d *DB) CompleteMCPOAuthFlow(ctx context.Context, externalID, credentialExternalID string, completedAt time.Time) error {
	mapper := NewMCPOAuthFlowMapper(d.mapperDB)
	rowsAffected, err := mapper.Complete(ctx, externalID, credentialExternalID, completedAt)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) FailMCPOAuthFlow(ctx context.Context, externalID, errorCode string, failedAt time.Time) error {
	mapper := NewMCPOAuthFlowMapper(d.mapperDB)
	rowsAffected, err := mapper.Fail(ctx, externalID, errorCode, failedAt)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func mcpOAuthFlowInsertParams(flow MCPOAuthFlow) (insertMCPOAuthFlowParams, error) {
	if err := requireCompleteSecretEnvelope(flow.SecretEnvelope); err != nil {
		return insertMCPOAuthFlowParams{}, err
	}
	ciphertext, nonce, wrappedDEK, formatVersion, keyProvider, keyVersion := vaultCredentialSecretColumns(flow.SecretEnvelope)
	return insertMCPOAuthFlowParams{
		UUID:                      flow.UUID,
		ExternalID:                flow.ExternalID,
		OrganizationUUID:          flow.OrganizationUUID,
		WorkspaceUUID:             flow.WorkspaceUUID,
		VaultUUID:                 flow.VaultUUID,
		VaultExternalID:           flow.VaultExternalID,
		UserUUID:                  optionalVaultString(flow.UserUUID),
		UserExternalID:            flow.UserExternalID,
		PlatformSessionExternalID: flow.PlatformSessionExternalID,
		MCPServerURL:              flow.MCPServerURL,
		RedirectURL:               flow.RedirectURL,
		DisplayName:               flow.DisplayName,
		Source:                    flow.Source,
		AuthorizationEndpoint:     flow.AuthorizationEndpoint,
		TokenEndpoint:             flow.TokenEndpoint,
		RegistrationEndpoint:      flow.RegistrationEndpoint,
		Issuer:                    flow.Issuer,
		Resource:                  flow.Resource,
		Scope:                     flow.Scope,
		ClientID:                  flow.ClientID,
		ClientCredentialSource:    flow.ClientCredentialSource,
		TokenEndpointAuthMethod:   flow.TokenEndpointAuthMethod,
		CodeChallengeMethod:       flow.CodeChallengeMethod,
		Ciphertext:                ciphertext,
		Nonce:                     nonce,
		WrappedDEK:                wrappedDEK,
		FormatVersion:             formatVersion,
		KeyProvider:               keyProvider,
		KeyVersion:                keyVersion,
		Status:                    flow.Status,
		CreatedAt:                 flow.CreatedAt,
		ExpiresAt:                 flow.ExpiresAt,
	}, nil
}

func (r mcpOAuthFlowRow) flow() MCPOAuthFlow {
	userUUID := ""
	if r.UserUUID.Valid {
		userUUID = r.UserUUID.String
	}
	flow := MCPOAuthFlow{
		UUID:                      r.UUID,
		ExternalID:                r.ExternalID,
		OrganizationUUID:          r.OrganizationUUID,
		WorkspaceUUID:             r.WorkspaceUUID,
		VaultUUID:                 r.VaultUUID,
		VaultExternalID:           r.VaultExternalID,
		UserUUID:                  userUUID,
		UserExternalID:            r.UserExternalID,
		PlatformSessionExternalID: r.PlatformSessionExternalID,
		MCPServerURL:              r.MCPServerURL,
		RedirectURL:               r.RedirectURL,
		DisplayName:               r.DisplayName,
		Source:                    r.Source,
		AuthorizationEndpoint:     r.AuthorizationEndpoint,
		TokenEndpoint:             r.TokenEndpoint,
		RegistrationEndpoint:      r.RegistrationEndpoint,
		Issuer:                    r.Issuer,
		Resource:                  r.Resource,
		Scope:                     r.Scope,
		ClientID:                  r.ClientID,
		ClientCredentialSource:    r.ClientCredentialSource,
		TokenEndpointAuthMethod:   r.TokenEndpointAuthMethod,
		CodeChallengeMethod:       r.CodeChallengeMethod,
		Status:                    r.Status,
		CredentialExternalID:      r.CredentialExternalID,
		ErrorCode:                 r.ErrorCode,
		CreatedAt:                 r.CreatedAt,
		UpdatedAt:                 r.UpdatedAt,
		ExpiresAt:                 r.ExpiresAt,
		CompletedAt:               r.CompletedAt,
	}
	if len(r.Ciphertext) > 0 || len(r.Nonce) > 0 || len(r.WrappedDEK) > 0 ||
		(r.FormatVersion.Valid && r.FormatVersion.Int32 != 0) ||
		(r.KeyProvider.Valid && r.KeyProvider.String != "") ||
		(r.KeyVersion.Valid && r.KeyVersion.Int64 != 0) {
		formatVersion := 0
		if r.FormatVersion.Valid {
			formatVersion = int(r.FormatVersion.Int32)
		}
		keyProvider := ""
		if r.KeyProvider.Valid {
			keyProvider = r.KeyProvider.String
		}
		keyVersion := int64(0)
		if r.KeyVersion.Valid {
			keyVersion = r.KeyVersion.Int64
		}
		flow.SecretEnvelope = &secrets.Envelope{
			Ciphertext:    append([]byte(nil), r.Ciphertext...),
			Nonce:         append([]byte(nil), r.Nonce...),
			WrappedDEK:    append([]byte(nil), r.WrappedDEK...),
			FormatVersion: formatVersion,
			KeyProvider:   keyProvider,
			KeyVersion:    keyVersion,
		}
	}
	return flow
}
