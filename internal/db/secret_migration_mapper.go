package db

import (
	"context"
	"encoding/json"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SecretMigrationMapper -sql ./secret_migration_mapper.xml -out ./secret_migration_mapper.sqlmap.gen.go -dialect postgres

type secretMigrationRow struct {
	UUID             string          `db:"uuid"`
	OrganizationUUID string          `db:"organization_uuid"`
	WorkspaceUUID    string          `db:"workspace_uuid"`
	Document         json.RawMessage `db:"document"`
}

type SecretMigrationMapper interface {
	ListPage(ctx context.Context, kind, after string, limit int) ([]secretMigrationRow, error)
	Replace(ctx context.Context, kind string, expected SecretMigrationRecord, next json.RawMessage) (int64, error)
}
