package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformOAuthProviderMapper -sql ./platform_oauth_provider_mapper.xml -out ./platform_oauth_provider_mapper.sqlmap.gen.go -dialect postgres

type platformOAuthProviderRow struct {
	UUID                       string    `db:"uuid"`
	ExternalID                 string    `db:"external_id"`
	Name                       string    `db:"name"`
	AuthorizeEndpoint          string    `db:"authorize_endpoint"`
	TokenEndpoint              string    `db:"token_endpoint"`
	Scope                      string    `db:"scope"`
	ClientID                   string    `db:"client_id"`
	ClientSecretEnvelope       string    `db:"client_secret_envelope"`
	TokenEndpointAuthMethod    string    `db:"token_endpoint_auth_method"`
	TokenEndpointEncoding      string    `db:"token_endpoint_encoding"`
	CreatedByPlatformAdminUUID string    `db:"created_by_platform_admin_uuid"`
	CreatedAt                  time.Time `db:"created_at"`
	UpdatedAt                  time.Time `db:"updated_at"`
}

type upsertPlatformOAuthProviderParams struct {
	UUID                       string
	ExternalID                 string
	Name                       string
	AuthorizeEndpoint          string
	TokenEndpoint              string
	Scope                      string
	ClientID                   string
	ClientSecretEnvelope       string
	TokenEndpointAuthMethod    string
	TokenEndpointEncoding      string
	CreatedByPlatformAdminUUID string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type PlatformOAuthProviderMapper interface {
	Insert(ctx context.Context, params upsertPlatformOAuthProviderParams) (platformOAuthProviderRow, error)
	List(ctx context.Context) ([]platformOAuthProviderRow, error)
	FindByExternalID(ctx context.Context, externalID string) (platformOAuthProviderRow, error)
	UpdateByExternalID(ctx context.Context, params upsertPlatformOAuthProviderParams) (platformOAuthProviderRow, error)
	DeleteByExternalID(ctx context.Context, externalID string) (int64, error)
}
