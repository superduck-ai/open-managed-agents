package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MemoryStoreMapper -sql ./memory_store_mapper.xml -out ./memory_store_mapper.sqlmap.gen.go -dialect postgres

type memoryStoreRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID *string    `db:"created_by_api_key_uuid"`
	Name                string     `db:"name"`
	Description         string     `db:"description"`
	Metadata            []byte     `db:"metadata"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type insertMemoryStoreParams struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID *string
	Name                string
	Description         string
	Metadata            []byte
	CreatedAt           time.Time
}

type updateMemoryStoreParams struct {
	WorkspaceUUID string
	ExternalID    string
	Name          string
	Description   string
	Metadata      []byte
	UpdatedAt     time.Time
}

type listMemoryStoresParams struct {
	WorkspaceUUID   string
	Limit           int
	IncludeArchived bool
	HasCreatedAtGTE bool
	CreatedAtGTE    time.Time
	HasCreatedAtLTE bool
	CreatedAtLTE    time.Time
	HasCursor       bool
	CursorCreatedAt time.Time
	CursorUUID      string
}

type MemoryStoreMapper interface {
	Insert(ctx context.Context, params insertMemoryStoreParams) (memoryStoreRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (memoryStoreRow, error)
	FindByOrganizationAndExternalID(ctx context.Context, organizationUUID, externalID string) (memoryStoreRow, error)
	FindForUpdate(ctx context.Context, workspaceUUID, externalID string) (memoryStoreRow, error)
	UpdateByExternalID(ctx context.Context, params updateMemoryStoreParams) (memoryStoreRow, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, externalID string) (memoryStoreRow, error)
	FindUUIDForUpdate(ctx context.Context, workspaceUUID, externalID string) (string, error)
	DeleteByUUID(ctx context.Context, workspaceUUID, storeUUID string) error
	ListPage(ctx context.Context, params listMemoryStoresParams) ([]memoryStoreRow, error)
	Exists(ctx context.Context, workspaceUUID, externalID string) (bool, error)
}
