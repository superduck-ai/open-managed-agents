package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper EnvironmentMapper -sql ./environment_mapper.xml -out ./environment_mapper.sqlmap.gen.go -dialect postgres

type environmentMapperRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID *string    `db:"created_by_api_key_uuid"`
	Name                string     `db:"name"`
	Description         string     `db:"description"`
	Config              []byte     `db:"config"`
	Metadata            []byte     `db:"metadata"`
	Scope               *string    `db:"scope"`
	Provider            string     `db:"provider"`
	ResolvedTemplate    string     `db:"resolved_template"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type environmentWriteParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID *string
	Name                string
	Description         string
	Config              []byte
	Metadata            []byte
	Scope               *string
	Provider            string
	ResolvedTemplate    string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type environmentPageMapperParams struct {
	WorkspaceUUID   string
	FetchLimit      int
	Cursor          *EnvironmentPageCursor
	IncludeArchived bool
}

type EnvironmentMapper interface {
	Insert(ctx context.Context, params environmentWriteParams) (environmentMapperRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (environmentMapperRow, error)
	FindByUUID(ctx context.Context, workspaceUUID, environmentUUID string) (environmentMapperRow, error)
	UpdateByExternalID(ctx context.Context, params environmentWriteParams) (environmentMapperRow, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, externalID string) (environmentMapperRow, error)
	LockUUIDByExternalID(ctx context.Context, workspaceUUID, externalID string) (string, error)
	SoftDeleteByUUID(ctx context.Context, workspaceUUID, environmentUUID string) (int64, error)
	ListPage(ctx context.Context, params environmentPageMapperParams) ([]environmentMapperRow, error)
}
