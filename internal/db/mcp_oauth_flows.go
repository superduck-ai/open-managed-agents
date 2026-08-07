package db

import (
	"context"
	"time"
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
	ClientSecret              string
	TokenEndpointAuthMethod   string
	CodeVerifier              string
	CodeChallengeMethod       string
	Status                    string
	CredentialExternalID      string
	ErrorCode                 string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	ExpiresAt                 time.Time
	CompletedAt               *time.Time
}

func (d *DB) CreateMCPOAuthFlow(ctx context.Context, flow MCPOAuthFlow) (MCPOAuthFlow, error) {
	mapper := NewMCPOAuthFlowMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, mcpOAuthFlowInsertParams(flow))
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

func mcpOAuthFlowInsertParams(flow MCPOAuthFlow) insertMCPOAuthFlowParams {
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
		ClientSecret:              flow.ClientSecret,
		TokenEndpointAuthMethod:   flow.TokenEndpointAuthMethod,
		CodeVerifier:              flow.CodeVerifier,
		CodeChallengeMethod:       flow.CodeChallengeMethod,
		Status:                    flow.Status,
		CreatedAt:                 flow.CreatedAt,
		ExpiresAt:                 flow.ExpiresAt,
	}
}

func (r mcpOAuthFlowRow) flow() MCPOAuthFlow {
	userUUID := ""
	if r.UserUUID.Valid {
		userUUID = r.UserUUID.String
	}
	return MCPOAuthFlow{
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
		ClientSecret:              r.ClientSecret,
		TokenEndpointAuthMethod:   r.TokenEndpointAuthMethod,
		CodeVerifier:              r.CodeVerifier,
		CodeChallengeMethod:       r.CodeChallengeMethod,
		Status:                    r.Status,
		CredentialExternalID:      r.CredentialExternalID,
		ErrorCode:                 r.ErrorCode,
		CreatedAt:                 r.CreatedAt,
		UpdatedAt:                 r.UpdatedAt,
		ExpiresAt:                 r.ExpiresAt,
		CompletedAt:               r.CompletedAt,
	}
}
