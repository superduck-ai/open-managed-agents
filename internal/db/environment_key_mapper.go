package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper EnvironmentKeyMapper -sql ./environment_key_mapper.xml -out ./environment_key_mapper.sqlmap.gen.go -dialect postgres

type environmentKeyMapperRow struct {
	UUID                  string `db:"uuid"`
	ExternalID            string `db:"external_id"`
	OrganizationUUID      string `db:"organization_uuid"`
	WorkspaceUUID         string `db:"workspace_uuid"`
	WorkspaceExternalID   string `db:"workspace_external_id"`
	EnvironmentUUID       string `db:"environment_uuid"`
	EnvironmentExternalID string `db:"environment_external_id"`
}

type environmentKeyUpsertParams struct {
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	EnvironmentUUID       string
	EnvironmentExternalID string
	KeyHash               string
}

type EnvironmentKeyMapper interface {
	Upsert(ctx context.Context, params environmentKeyUpsertParams) error
	FindAndTouchByHash(ctx context.Context, keyHash string) (environmentKeyMapperRow, error)
}
