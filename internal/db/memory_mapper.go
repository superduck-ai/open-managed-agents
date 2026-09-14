package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MemoryMapper -sql ./memory_mapper.xml -out ./memory_mapper.sqlmap.gen.go -dialect postgres

type memoryRow struct {
	UUID                     string     `db:"uuid"`
	ExternalID               string     `db:"external_id"`
	OrganizationUUID         string     `db:"organization_uuid"`
	WorkspaceUUID            string     `db:"workspace_uuid"`
	MemoryStoreUUID          string     `db:"memory_store_uuid"`
	MemoryStoreExternalID    string     `db:"memory_store_external_id"`
	CurrentVersionUUID       *string    `db:"current_version_uuid"`
	CurrentVersionExternalID string     `db:"current_version_external_id"`
	Path                     string     `db:"path"`
	ContentSizeBytes         int64      `db:"content_size_bytes"`
	ContentSHA256            string     `db:"content_sha256"`
	S3Bucket                 string     `db:"s3_bucket"`
	S3Key                    string     `db:"s3_key"`
	CreatedAt                time.Time  `db:"created_at"`
	UpdatedAt                time.Time  `db:"updated_at"`
	DeletedAt                *time.Time `db:"deleted_at"`
}

type insertMemoryParams struct {
	UUID                     string
	ExternalID               string
	OrganizationUUID         string
	WorkspaceUUID            string
	MemoryStoreUUID          string
	MemoryStoreExternalID    string
	CurrentVersionExternalID string
	Path                     string
	ContentSizeBytes         int64
	ContentSHA256            string
	S3Bucket                 string
	S3Key                    string
	CreatedAt                time.Time
}

type updateMemoryCurrentVersionParams struct {
	WorkspaceUUID     string
	MemoryUUID        string
	VersionUUID       string
	VersionExternalID string
}

type updateMemoryParams struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	MemoryExternalID      string
	VersionUUID           string
	VersionExternalID     string
	Path                  string
	ContentSizeBytes      int64
	ContentSHA256         string
	S3Bucket              string
	S3Key                 string
	UpdatedAt             time.Time
}

type deleteMemoryParams struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	MemoryExternalID      string
	VersionUUID           string
	VersionExternalID     string
	UpdatedAt             time.Time
}

type listMemoriesParams struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	Limit                 int
	PathPrefix            string
	HasCursor             bool
	CursorPath            string
	CursorCreatedAt       time.Time
	CursorUpdatedAt       time.Time
	CursorUUID            string
	OrderBy               string
	Descending            bool
}

type listMemoriesForDepthParams struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	PathPrefix            string
}

type MemoryMapper interface {
	Insert(ctx context.Context, params insertMemoryParams) (memoryRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, memoryStoreExternalID, memoryExternalID string) (memoryRow, error)
	FindForUpdate(ctx context.Context, workspaceUUID, memoryStoreExternalID, memoryExternalID string) (memoryRow, error)
	UpdateCurrentVersion(ctx context.Context, params updateMemoryCurrentVersionParams) (memoryRow, error)
	UpdateByExternalID(ctx context.Context, params updateMemoryParams) (memoryRow, error)
	SoftDeleteByExternalID(ctx context.Context, params deleteMemoryParams) (int64, error)
	ListPage(ctx context.Context, params listMemoriesParams) ([]memoryRow, error)
	ListForDepth(ctx context.Context, params listMemoriesForDepthParams) ([]memoryRow, error)
	FindPathConflict(ctx context.Context, workspaceUUID, storeUUID, path, excludeMemoryUUID string) (string, bool, error)
	CountActiveHead(ctx context.Context, workspaceUUID, memoryStoreExternalID, versionUUID string) (int, error)
	CountActive(ctx context.Context, workspaceUUID, memoryStoreExternalID string) (int, error)
	DeleteByStoreUUID(ctx context.Context, workspaceUUID, storeUUID string) error
}
