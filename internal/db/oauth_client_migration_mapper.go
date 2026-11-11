package db

import (
	"context"
	"database/sql"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper OAuthClientMigrationMapper -sql ./oauth_client_migration_mapper.xml -out ./oauth_client_migration_mapper.sqlmap.gen.go -dialect postgres

type oauthClientMigrationRow struct {
	UUID                 string         `db:"uuid"`
	ExternalID           string         `db:"external_id"`
	OrganizationUUID     string         `db:"organization_uuid"`
	WorkspaceUUID        string         `db:"workspace_uuid"`
	LegacyClientID       string         `db:"legacy_client_id"`
	ClientIDEnvelope     sql.NullString `db:"client_id_envelope"`
	ClientSecretEnvelope sql.NullString `db:"client_secret_envelope"`
}

type OAuthClientMigrationMapper interface {
	ListPage(ctx context.Context, kind, after string, transitional, expanded bool) ([]oauthClientMigrationRow, error)
	Backfill(ctx context.Context, kind string, expected OAuthClientMigrationRecord, envelope string) (int64, error)
}
