package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EnvOAuthFlowMapper -sql ./env_oauth_flow_mapper.xml -out ./env_oauth_flow_mapper.sqlmap.gen.go -dialect postgres

type envOAuthFlowRow struct {
	UUID                    string         `db:"uuid"`
	ExternalID              string         `db:"external_id"`
	OrganizationUUID        string         `db:"organization_uuid"`
	WorkspaceUUID           string         `db:"workspace_uuid"`
	VaultUUID               string         `db:"vault_uuid"`
	VaultExternalID         string         `db:"vault_external_id"`
	ProviderKey             string         `db:"provider_key"`
	SecretName              string         `db:"secret_name"`
	DisplayName             string         `db:"display_name"`
	RedirectURL             string         `db:"redirect_url"`
	AuthorizeEndpoint       string         `db:"authorize_endpoint"`
	TokenEndpoint           string         `db:"token_endpoint"`
	ClientID                string         `db:"client_id"`
	Scope                   string         `db:"scope"`
	TokenEndpointAuthMethod string         `db:"token_endpoint_auth_method"`
	TokenEndpointEncoding   string         `db:"token_endpoint_encoding"`
	NetworkingType          string         `db:"networking_type"`
	AllowedHosts            string         `db:"allowed_hosts"`
	Status                  string         `db:"status"`
	ErrorCode               string         `db:"error_code"`
	CredentialExternalID    string         `db:"credential_external_id"`
	CreatedByAPIKeyUUID     sql.NullString `db:"created_by_api_key_uuid"`
	ExpiresAt               time.Time      `db:"expires_at"`
	CompletedAt             *time.Time     `db:"completed_at"`
	CreatedAt               time.Time      `db:"created_at"`
	UpdatedAt               time.Time      `db:"updated_at"`
}

type insertEnvOAuthFlowParams struct {
	UUID                    string
	ExternalID              string
	OrganizationUUID        string
	WorkspaceUUID           string
	VaultUUID               string
	VaultExternalID         string
	ProviderKey             string
	SecretName              string
	DisplayName             string
	RedirectURL             string
	AuthorizeEndpoint       string
	TokenEndpoint           string
	ClientID                string
	Scope                   string
	TokenEndpointAuthMethod string
	TokenEndpointEncoding   string
	NetworkingType          string
	AllowedHosts            string
	Status                  string
	CreatedByAPIKeyUUID     *string
	CreatedAt               time.Time
	ExpiresAt               time.Time
}

type EnvOAuthFlowMapper interface {
	Insert(ctx context.Context, params insertEnvOAuthFlowParams) (envOAuthFlowRow, error)
	FindByExternalID(ctx context.Context, externalID string) (envOAuthFlowRow, error)
	Complete(ctx context.Context, externalID, credentialExternalID string, completedAt time.Time) (int64, error)
	Fail(ctx context.Context, externalID, errorCode string, failedAt time.Time) (int64, error)
}
