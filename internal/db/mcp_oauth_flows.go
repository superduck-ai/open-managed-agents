package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

const (
	createMCPOAuthFlowQuery = `
		insert into mcp_oauth_flows (
			uuid, external_id, organization_uuid, workspace_uuid, vault_uuid, vault_external_id,
			user_uuid, user_external_id, platform_session_external_id, mcp_server_url,
			redirect_url, display_name, source, authorization_endpoint, token_endpoint,
			registration_endpoint, issuer, resource, scope, client_id, client_secret,
			token_endpoint_auth_method, code_verifier, code_challenge_method, status,
			created_at, updated_at, expires_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :vault_uuid, :vault_external_id,
			:user_uuid, nullif(:user_external_id, ''),
			nullif(:platform_session_external_id, ''), :mcp_server_url,
			:redirect_url, :display_name, :source, :authorization_endpoint, :token_endpoint,
			nullif(:registration_endpoint, ''), nullif(:issuer, ''), :resource,
			nullif(:scope, ''), :client_id, nullif(:client_secret, ''),
			:token_endpoint_auth_method, :code_verifier, :code_challenge_method, :status,
			:created_at, :created_at, :expires_at
		)
		returning ` + mcpOAuthFlowReturnColumns + `
	`
	getMCPOAuthFlowQuery = `
		select ` + mcpOAuthFlowReturnColumns + `
		from mcp_oauth_flows
		where external_id = :external_id
	`
	completeMCPOAuthFlowQuery = `
		update mcp_oauth_flows
		set status = 'completed',
			credential_external_id = :credential_external_id,
			error_code = null,
			client_secret = null,
			code_verifier = '',
			completed_at = :completed_at,
			updated_at = :completed_at
		where external_id = :external_id and status = 'pending'
	`
	failMCPOAuthFlowQuery = `
		update mcp_oauth_flows
		set status = 'failed',
			error_code = :error_code,
			client_secret = null,
			code_verifier = '',
			updated_at = :failed_at
		where external_id = :external_id and status = 'pending'
	`
	mcpOAuthFlowReturnColumns = `
		uuid,
		external_id,
		organization_uuid,
		workspace_uuid,
		vault_uuid,
		vault_external_id,
		user_uuid,
		coalesce(user_external_id, '') AS user_external_id,
		coalesce(platform_session_external_id, '') AS platform_session_external_id,
		mcp_server_url,
		redirect_url,
		display_name,
		source,
		authorization_endpoint,
		token_endpoint,
		coalesce(registration_endpoint, '') AS registration_endpoint,
		coalesce(issuer, '') AS issuer,
		resource,
		coalesce(scope, '') AS scope,
		client_id,
		coalesce(client_secret, '') AS client_secret,
		token_endpoint_auth_method,
		code_verifier,
		code_challenge_method,
		status,
		coalesce(credential_external_id, '') AS credential_external_id,
		coalesce(error_code, '') AS error_code,
		created_at,
		updated_at,
		expires_at,
		completed_at
	`
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

type mcpOAuthFlowRow struct {
	UUID                      uuid.UUID     `db:"uuid"`
	ExternalID                string        `db:"external_id"`
	OrganizationUUID          uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID             uuid.UUID     `db:"workspace_uuid"`
	VaultUUID                 uuid.UUID     `db:"vault_uuid"`
	VaultExternalID           string        `db:"vault_external_id"`
	UserUUID                  uuid.NullUUID `db:"user_uuid"`
	UserExternalID            string        `db:"user_external_id"`
	PlatformSessionExternalID string        `db:"platform_session_external_id"`
	MCPServerURL              string        `db:"mcp_server_url"`
	RedirectURL               string        `db:"redirect_url"`
	DisplayName               string        `db:"display_name"`
	Source                    string        `db:"source"`
	AuthorizationEndpoint     string        `db:"authorization_endpoint"`
	TokenEndpoint             string        `db:"token_endpoint"`
	RegistrationEndpoint      string        `db:"registration_endpoint"`
	Issuer                    string        `db:"issuer"`
	Resource                  string        `db:"resource"`
	Scope                     string        `db:"scope"`
	ClientID                  string        `db:"client_id"`
	ClientSecret              string        `db:"client_secret"`
	TokenEndpointAuthMethod   string        `db:"token_endpoint_auth_method"`
	CodeVerifier              string        `db:"code_verifier"`
	CodeChallengeMethod       string        `db:"code_challenge_method"`
	Status                    string        `db:"status"`
	CredentialExternalID      string        `db:"credential_external_id"`
	ErrorCode                 string        `db:"error_code"`
	CreatedAt                 time.Time     `db:"created_at"`
	UpdatedAt                 time.Time     `db:"updated_at"`
	ExpiresAt                 time.Time     `db:"expires_at"`
	CompletedAt               *time.Time    `db:"completed_at"`
}

func (d *DB) CreateMCPOAuthFlow(ctx context.Context, flow MCPOAuthFlow) (MCPOAuthFlow, error) {
	return getMCPOAuthFlowSQLX(ctx, d.sql, createMCPOAuthFlowQuery, mcpOAuthFlowArguments(flow))
}

func (d *DB) GetMCPOAuthFlow(ctx context.Context, externalID string) (MCPOAuthFlow, error) {
	return getMCPOAuthFlowSQLX(ctx, d.sql, getMCPOAuthFlowQuery, map[string]any{
		"external_id": externalID,
	})
}

func (d *DB) CompleteMCPOAuthFlow(ctx context.Context, externalID, credentialExternalID string, completedAt time.Time) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, completeMCPOAuthFlowQuery, map[string]any{
		"external_id":            externalID,
		"credential_external_id": credentialExternalID,
		"completed_at":           completedAt,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) FailMCPOAuthFlow(ctx context.Context, externalID, errorCode string, failedAt time.Time) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, failMCPOAuthFlowQuery, map[string]any{
		"external_id": externalID,
		"error_code":  errorCode,
		"failed_at":   failedAt,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func getMCPOAuthFlowSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (MCPOAuthFlow, error) {
	var row mcpOAuthFlowRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return MCPOAuthFlow{}, mapNoRows(err)
	}
	return row.flow(), nil
}

func mcpOAuthFlowArguments(flow MCPOAuthFlow) map[string]any {
	return map[string]any{
		"uuid":                         dbUUID(flow.UUID),
		"external_id":                  flow.ExternalID,
		"organization_uuid":            dbUUID(flow.OrganizationUUID),
		"workspace_uuid":               dbUUID(flow.WorkspaceUUID),
		"vault_uuid":                   dbUUID(flow.VaultUUID),
		"vault_external_id":            flow.VaultExternalID,
		"user_uuid":                    dbNullableUUID(&flow.UserUUID),
		"user_external_id":             flow.UserExternalID,
		"platform_session_external_id": flow.PlatformSessionExternalID,
		"mcp_server_url":               flow.MCPServerURL,
		"redirect_url":                 flow.RedirectURL,
		"display_name":                 flow.DisplayName,
		"source":                       flow.Source,
		"authorization_endpoint":       flow.AuthorizationEndpoint,
		"token_endpoint":               flow.TokenEndpoint,
		"registration_endpoint":        flow.RegistrationEndpoint,
		"issuer":                       flow.Issuer,
		"resource":                     flow.Resource,
		"scope":                        flow.Scope,
		"client_id":                    flow.ClientID,
		"client_secret":                flow.ClientSecret,
		"token_endpoint_auth_method":   flow.TokenEndpointAuthMethod,
		"code_verifier":                flow.CodeVerifier,
		"code_challenge_method":        flow.CodeChallengeMethod,
		"status":                       flow.Status,
		"created_at":                   flow.CreatedAt,
		"expires_at":                   flow.ExpiresAt,
	}
}

func (r mcpOAuthFlowRow) flow() MCPOAuthFlow {
	return MCPOAuthFlow{
		UUID:                      r.UUID.String(),
		ExternalID:                r.ExternalID,
		OrganizationUUID:          r.OrganizationUUID.String(),
		WorkspaceUUID:             r.WorkspaceUUID.String(),
		VaultUUID:                 r.VaultUUID.String(),
		VaultExternalID:           r.VaultExternalID,
		UserUUID:                  nullableUUIDValue(r.UserUUID),
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
