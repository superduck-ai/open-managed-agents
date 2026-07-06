package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type MCPOAuthFlow struct {
	ID                        int64
	UUID                      string
	ExternalID                string
	OrganizationID            int64
	WorkspaceID               int64
	VaultID                   int64
	VaultExternalID           string
	UserID                    int64
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
	return scanMCPOAuthFlow(d.Pool.QueryRow(ctx, `
		insert into mcp_oauth_flows (
			uuid, external_id, organization_id, workspace_id, vault_id, vault_external_id,
			user_id, user_external_id, platform_session_external_id, mcp_server_url,
			redirect_url, display_name, source, authorization_endpoint, token_endpoint,
			registration_endpoint, issuer, resource, scope, client_id, client_secret,
			token_endpoint_auth_method, code_verifier, code_challenge_method, status,
			created_at, updated_at, expires_at
		)
		values (
			$1, $2, $3, $4, $5, $6,
			nullif($7, 0), nullif($8, ''), nullif($9, ''), $10,
			$11, $12, $13, $14, $15,
			nullif($16, ''), nullif($17, ''), $18, nullif($19, ''), $20, nullif($21, ''),
			$22, $23, $24, $25,
			$26, $26, $27
		)
		returning `+mcpOAuthFlowReturnColumns(), flow.UUID, flow.ExternalID,
		flow.OrganizationID, flow.WorkspaceID, flow.VaultID, flow.VaultExternalID,
		flow.UserID, flow.UserExternalID, flow.PlatformSessionExternalID,
		flow.MCPServerURL, flow.RedirectURL, flow.DisplayName, flow.Source,
		flow.AuthorizationEndpoint, flow.TokenEndpoint, flow.RegistrationEndpoint,
		flow.Issuer, flow.Resource, flow.Scope, flow.ClientID, flow.ClientSecret,
		flow.TokenEndpointAuthMethod, flow.CodeVerifier, flow.CodeChallengeMethod,
		flow.Status, flow.CreatedAt, flow.ExpiresAt))
}

func (d *DB) GetMCPOAuthFlow(ctx context.Context, externalID string) (MCPOAuthFlow, error) {
	return scanMCPOAuthFlow(d.Pool.QueryRow(ctx, `
		select `+mcpOAuthFlowReturnColumns()+`
		from mcp_oauth_flows
		where external_id = $1
	`, externalID))
}

func (d *DB) CompleteMCPOAuthFlow(ctx context.Context, externalID, credentialExternalID string, completedAt time.Time) error {
	tag, err := d.Pool.Exec(ctx, `
		update mcp_oauth_flows
		set status = 'completed',
			credential_external_id = $2,
			error_code = null,
			client_secret = null,
			code_verifier = '',
			completed_at = $3,
			updated_at = $3
		where external_id = $1 and status = 'pending'
	`, externalID, credentialExternalID, completedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) FailMCPOAuthFlow(ctx context.Context, externalID, errorCode string, failedAt time.Time) error {
	tag, err := d.Pool.Exec(ctx, `
		update mcp_oauth_flows
		set status = 'failed',
			error_code = $2,
			client_secret = null,
			code_verifier = '',
			updated_at = $3
		where external_id = $1 and status = 'pending'
	`, externalID, errorCode, failedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func mcpOAuthFlowReturnColumns() string {
	return `
		id, uuid::text, external_id, organization_id, workspace_id, vault_id,
		vault_external_id, coalesce(user_id, 0), coalesce(user_external_id, ''),
		coalesce(platform_session_external_id, ''), mcp_server_url, redirect_url,
		display_name, source, authorization_endpoint, token_endpoint,
		coalesce(registration_endpoint, ''), coalesce(issuer, ''), resource,
		coalesce(scope, ''), client_id, coalesce(client_secret, ''),
		token_endpoint_auth_method, code_verifier, code_challenge_method, status,
		coalesce(credential_external_id, ''), coalesce(error_code, ''),
		created_at, updated_at, expires_at, completed_at
	`
}

func scanMCPOAuthFlow(row vaultScanner) (MCPOAuthFlow, error) {
	var flow MCPOAuthFlow
	err := row.Scan(&flow.ID, &flow.UUID, &flow.ExternalID, &flow.OrganizationID,
		&flow.WorkspaceID, &flow.VaultID, &flow.VaultExternalID, &flow.UserID,
		&flow.UserExternalID, &flow.PlatformSessionExternalID, &flow.MCPServerURL,
		&flow.RedirectURL, &flow.DisplayName, &flow.Source, &flow.AuthorizationEndpoint,
		&flow.TokenEndpoint, &flow.RegistrationEndpoint, &flow.Issuer, &flow.Resource,
		&flow.Scope, &flow.ClientID, &flow.ClientSecret, &flow.TokenEndpointAuthMethod,
		&flow.CodeVerifier, &flow.CodeChallengeMethod, &flow.Status,
		&flow.CredentialExternalID, &flow.ErrorCode, &flow.CreatedAt, &flow.UpdatedAt,
		&flow.ExpiresAt, &flow.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MCPOAuthFlow{}, ErrNotFound
	}
	if err != nil {
		return MCPOAuthFlow{}, err
	}
	return flow, nil
}
