package db

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
)

var secretMigrationKinds = []string{"vault_credentials", "mcp_oauth_flows", "mcp_tunnel_token_versions", "llm_providers", "session_resources", "deployments"}

type SecretMigrationRecord struct {
	UUID             string
	OrganizationUUID string
	WorkspaceUUID    string
	Document         json.RawMessage
}

func SecretMigrationKinds() []string { return slices.Clone(secretMigrationKinds) }

func (d *DB) ListSecretMigrationPage(ctx context.Context, kind, after string) ([]SecretMigrationRecord, error) {
	if !slices.Contains(secretMigrationKinds, kind) {
		return nil, errors.New("unknown secret storage kind")
	}
	rows, err := NewSecretMigrationMapper(d.mapperDB).ListPage(ctx, kind, after, 100)
	if err != nil {
		return nil, err
	}
	result := make([]SecretMigrationRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, SecretMigrationRecord(row))
	}
	return result, nil
}

func (d *DB) SaveMigratedSecret(ctx context.Context, kind string, expected SecretMigrationRecord, next json.RawMessage) error {
	if !slices.Contains(secretMigrationKinds, kind) {
		return errors.New("unknown secret storage kind")
	}
	if expected.OrganizationUUID == "" || expected.WorkspaceUUID == "" || expected.UUID == "" {
		return errors.New("secret migration scope is required")
	}
	rows, err := NewSecretMigrationMapper(d.mapperDB).Replace(ctx, kind, expected, next)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrVersionConflict
	}
	return nil
}
