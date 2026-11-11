package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformOAuthProviderMapper -sql ./platform_oauth_provider_mapper.xml -out ./platform_oauth_provider_mapper.sqlmap.gen.go -dialect postgres

type platformOAuthProviderRow struct {
	UUID                       string         `db:"uuid"`
	ExternalID                 string         `db:"external_id"`
	Name                       string         `db:"name"`
	AuthorizeEndpoint          string         `db:"authorize_endpoint"`
	TokenEndpoint              string         `db:"token_endpoint"`
	Scope                      string         `db:"scope"`
	BuiltinKey                 sql.NullString `db:"builtin_key"`
	Protected                  bool           `db:"protected"`
	Version                    int64          `db:"version"`
	DefaultSecretName          string         `db:"default_secret_name"`
	DefaultAllowedHosts        string         `db:"default_allowed_hosts"`
	ClientIDEnvelope           sql.NullString `db:"client_id_envelope"`
	ClientSecretEnvelope       sql.NullString `db:"client_secret_envelope"`
	TokenEndpointAuthMethod    string         `db:"token_endpoint_auth_method"`
	TokenEndpointEncoding      string         `db:"token_endpoint_encoding"`
	CreatedByPlatformAdminUUID sql.NullString `db:"created_by_platform_admin_uuid"`
	CreatedAt                  time.Time      `db:"created_at"`
	UpdatedAt                  time.Time      `db:"updated_at"`
}

type upsertPlatformOAuthProviderParams struct {
	UUID                       string
	ExternalID                 string
	Name                       string
	AuthorizeEndpoint          string
	TokenEndpoint              string
	Scope                      string
	ClientIDEnvelope           *string
	Version                    int64
	ClientSecretEnvelope       *string
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
	FindByBuiltinKey(ctx context.Context, builtinKey string) (platformOAuthProviderRow, error)
	FindByUUID(ctx context.Context, providerUUID string) (platformOAuthProviderRow, error)
	UpdateByUUID(ctx context.Context, params upsertPlatformOAuthProviderParams) (platformOAuthProviderRow, error)
	DeleteByUUID(ctx context.Context, providerUUID string, expectedVersion int64) (int64, error)
}
