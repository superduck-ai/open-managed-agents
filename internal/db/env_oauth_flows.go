package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
	"time"
)

type EnvOAuthFlow struct {
	Continuation            *OAuthContinuation
	UUID                    string
	ExternalID              string
	OrganizationUUID        string
	WorkspaceUUID           string
	VaultUUID               string
	VaultExternalID         string
	ProviderVersion         int64
	ProviderKey             string
	SecretName              string
	DisplayName             string
	RedirectURL             string
	AuthorizeEndpoint       string
	TokenEndpoint           string
	ClientIDEnvelope        *secrets.Envelope
	CodeVerifierEnvelope    *secrets.Envelope
	Scope                   string
	TokenEndpointAuthMethod string
	TokenEndpointEncoding   string
	NetworkingType          string
	AllowedHosts            []string
	Status                  string
	ErrorCode               string
	CredentialExternalID    string
	CreatedByAPIKeyUUID     string
	ExpiresAt               time.Time
	CompletedAt             *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (d *DB) CreateEnvOAuthFlow(ctx context.Context, flow EnvOAuthFlow) (EnvOAuthFlow, error) {
	if flow.AllowedHosts == nil {
		flow.AllowedHosts = []string{}
	}
	allowedHosts, err := json.Marshal(flow.AllowedHosts)
	if err != nil {
		return EnvOAuthFlow{}, fmt.Errorf("encode env oauth allowed hosts: %w", err)
	}
	clientIDEnvelope, err := encodeOAuthEnvelope(flow.ClientIDEnvelope)
	if err != nil {
		return EnvOAuthFlow{}, err
	}
	verifierEnvelope, err := encodeOAuthEnvelope(flow.CodeVerifierEnvelope)
	if err != nil {
		return EnvOAuthFlow{}, err
	}
	continuation, err := oauthContinuationJSON(flow.Continuation)
	if err != nil {
		return EnvOAuthFlow{}, err
	}
	mapper := NewEnvOAuthFlowMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, insertEnvOAuthFlowParams{
		Continuation:            continuation,
		UUID:                    flow.UUID,
		ExternalID:              flow.ExternalID,
		OrganizationUUID:        flow.OrganizationUUID,
		WorkspaceUUID:           flow.WorkspaceUUID,
		VaultUUID:               flow.VaultUUID,
		VaultExternalID:         flow.VaultExternalID,
		ProviderVersion:         flow.ProviderVersion,
		ProviderKey:             flow.ProviderKey,
		SecretName:              flow.SecretName,
		DisplayName:             flow.DisplayName,
		RedirectURL:             flow.RedirectURL,
		AuthorizeEndpoint:       flow.AuthorizeEndpoint,
		TokenEndpoint:           flow.TokenEndpoint,
		ClientIDEnvelope:        clientIDEnvelope,
		CodeVerifierEnvelope:    verifierEnvelope,
		Scope:                   flow.Scope,
		TokenEndpointAuthMethod: flow.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   flow.TokenEndpointEncoding,
		NetworkingType:          flow.NetworkingType,
		AllowedHosts:            string(allowedHosts),
		CredentialExternalID:    flow.CredentialExternalID,
		Status:                  flow.Status,
		CreatedByAPIKeyUUID:     optionalVaultString(flow.CreatedByAPIKeyUUID),
		CreatedAt:               flow.CreatedAt,
		ExpiresAt:               flow.ExpiresAt,
	})
	if err != nil {
		return EnvOAuthFlow{}, err
	}
	return row.flow()
}

func (d *DB) GetEnvOAuthFlow(ctx context.Context, externalID string) (EnvOAuthFlow, error) {
	mapper := NewEnvOAuthFlowMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, externalID)
	if err != nil {
		return EnvOAuthFlow{}, mapNoRows(err)
	}
	return row.flow()
}

func (d *DB) ClaimEnvOAuthFlow(ctx context.Context, externalID string, claimedAt time.Time) (bool, error) {
	mapper := NewEnvOAuthFlowMapper(d.mapperDB)
	rowsAffected, err := mapper.Claim(ctx, externalID, claimedAt)
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (d *DB) CompleteEnvOAuthIdentity(ctx context.Context, externalID string, completedAt time.Time) error {
	affected, err := NewEnvOAuthFlowMapper(d.mapperDB).Complete(ctx, externalID, "", completedAt)
	if err == nil && affected != 1 {
		return ErrNotFound
	}
	return err
}

func (d *DB) SaveEnvOAuthCredential(ctx context.Context, flowID string, credential VaultCredential, existing bool) (VaultCredential, error) {
	var saved VaultCredential
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		affected, err := NewEnvOAuthFlowMapper(executor).Complete(ctx, flowID, credential.ExternalID, credential.UpdatedAt)
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrNotFound
		}
		if !existing {
			saved, err = createVaultCredential(ctx, executor, credential)
			return err
		}
		if err := requireCompleteSecretEnvelope(credential.SecretEnvelope); err != nil {
			return err
		}
		row, err := NewVaultCredentialMapper(executor).UpdateByExternalID(ctx, vaultCredentialUpdateParams(credential.WorkspaceUUID, credential.VaultExternalID, credential.ExternalID, credential))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		saved = row.credential()
		return nil
	})
	return saved, err
}

func (d *DB) FailEnvOAuthFlow(ctx context.Context, externalID, errorCode string, failedAt time.Time) error {
	mapper := NewEnvOAuthFlowMapper(d.mapperDB)
	rowsAffected, err := mapper.Fail(ctx, externalID, errorCode, failedAt)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r envOAuthFlowRow) flow() (EnvOAuthFlow, error) {
	continuation, err := decodeOAuthContinuation(r.Continuation)
	if err != nil {
		return EnvOAuthFlow{}, err
	}
	allowedHosts := make([]string, 0)
	if r.AllowedHosts != "" {
		if err := json.Unmarshal([]byte(r.AllowedHosts), &allowedHosts); err != nil {
			return EnvOAuthFlow{}, fmt.Errorf("decode env oauth allowed hosts: %w", err)
		}
	}
	createdByAPIKeyUUID := ""
	if r.CreatedByAPIKeyUUID.Valid {
		createdByAPIKeyUUID = r.CreatedByAPIKeyUUID.String
	}
	envelope, err := decodeOAuthEnvelope(r.ClientIDEnvelope)
	if err != nil {
		return EnvOAuthFlow{}, err
	}
	verifierEnvelope, err := decodeOAuthEnvelope(r.CodeVerifierEnvelope)
	if err != nil {
		return EnvOAuthFlow{}, err
	}
	return EnvOAuthFlow{
		Continuation:            continuation,
		UUID:                    r.UUID,
		ExternalID:              r.ExternalID,
		OrganizationUUID:        r.OrganizationUUID,
		WorkspaceUUID:           r.WorkspaceUUID,
		VaultUUID:               r.VaultUUID,
		VaultExternalID:         r.VaultExternalID,
		ProviderVersion:         r.ProviderVersion,
		ProviderKey:             r.ProviderKey,
		SecretName:              r.SecretName,
		DisplayName:             r.DisplayName,
		RedirectURL:             r.RedirectURL,
		AuthorizeEndpoint:       r.AuthorizeEndpoint,
		TokenEndpoint:           r.TokenEndpoint,
		ClientIDEnvelope:        envelope,
		CodeVerifierEnvelope:    verifierEnvelope,
		Scope:                   r.Scope,
		TokenEndpointAuthMethod: r.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   r.TokenEndpointEncoding,
		NetworkingType:          r.NetworkingType,
		AllowedHosts:            allowedHosts,
		Status:                  r.Status,
		ErrorCode:               r.ErrorCode,
		CredentialExternalID:    r.CredentialExternalID,
		CreatedByAPIKeyUUID:     createdByAPIKeyUUID,
		ExpiresAt:               r.ExpiresAt,
		CompletedAt:             r.CompletedAt,
		CreatedAt:               r.CreatedAt,
		UpdatedAt:               r.UpdatedAt,
	}, nil
}
