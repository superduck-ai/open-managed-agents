package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MCPOAuthFlowMapper -sql ./mcp_oauth_flow_mapper.xml -out ./mcp_oauth_flow_mapper.sqlmap.gen.go -dialect postgres

type mcpOAuthFlowRow struct {
	UUID                      string         `db:"uuid"`
	ExternalID                string         `db:"external_id"`
	OrganizationUUID          string         `db:"organization_uuid"`
	WorkspaceUUID             string         `db:"workspace_uuid"`
	VaultUUID                 string         `db:"vault_uuid"`
	VaultExternalID           string         `db:"vault_external_id"`
	UserUUID                  sql.NullString `db:"user_uuid"`
	UserExternalID            string         `db:"user_external_id"`
	PlatformSessionExternalID string         `db:"platform_session_external_id"`
	MCPServerURL              string         `db:"mcp_server_url"`
	RedirectURL               string         `db:"redirect_url"`
	DisplayName               string         `db:"display_name"`
	Source                    string         `db:"source"`
	AuthorizationEndpoint     string         `db:"authorization_endpoint"`
	TokenEndpoint             string         `db:"token_endpoint"`
	RegistrationEndpoint      string         `db:"registration_endpoint"`
	Issuer                    string         `db:"issuer"`
	Resource                  string         `db:"resource"`
	Scope                     string         `db:"scope"`
	ClientID                  string         `db:"client_id"`
	ClientCredentialSource    string         `db:"client_credential_source"`
	TokenEndpointAuthMethod   string         `db:"token_endpoint_auth_method"`
	CodeChallengeMethod       string         `db:"code_challenge_method"`
	Ciphertext                []byte         `db:"ciphertext"`
	Nonce                     []byte         `db:"nonce"`
	WrappedDEK                []byte         `db:"wrapped_dek"`
	FormatVersion             sql.NullInt32  `db:"format_version"`
	KeyProvider               sql.NullString `db:"key_provider"`
	KeyVersion                sql.NullInt64  `db:"key_version"`
	Status                    string         `db:"status"`
	CredentialExternalID      string         `db:"credential_external_id"`
	ErrorCode                 string         `db:"error_code"`
	CreatedAt                 time.Time      `db:"created_at"`
	UpdatedAt                 time.Time      `db:"updated_at"`
	ExpiresAt                 time.Time      `db:"expires_at"`
	CompletedAt               *time.Time     `db:"completed_at"`
}

type insertMCPOAuthFlowParams struct {
	UUID                      string
	ExternalID                string
	OrganizationUUID          string
	WorkspaceUUID             string
	VaultUUID                 string
	VaultExternalID           string
	UserUUID                  *string
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
	Ciphertext                []byte
	Nonce                     []byte
	WrappedDEK                []byte
	FormatVersion             *int32
	KeyProvider               *string
	KeyVersion                *int64
	Status                    string
	CreatedAt                 time.Time
	ExpiresAt                 time.Time
}

type MCPOAuthFlowMapper interface {
	Insert(ctx context.Context, params insertMCPOAuthFlowParams) (mcpOAuthFlowRow, error)
	FindByExternalID(ctx context.Context, externalID string) (mcpOAuthFlowRow, error)
	Complete(ctx context.Context, externalID, credentialExternalID string, completedAt time.Time) (int64, error)
	Fail(ctx context.Context, externalID, errorCode string, failedAt time.Time) (int64, error)
}
