package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MemoryVersionMapper -sql ./memory_version_mapper.xml -out ./memory_version_mapper.sqlmap.gen.go -dialect postgres

type memoryVersionRow struct {
	UUID                       string     `db:"uuid"`
	ExternalID                 string     `db:"external_id"`
	OrganizationUUID           string     `db:"organization_uuid"`
	WorkspaceUUID              string     `db:"workspace_uuid"`
	MemoryStoreUUID            string     `db:"memory_store_uuid"`
	MemoryStoreExternalID      string     `db:"memory_store_external_id"`
	MemoryUUID                 string     `db:"memory_uuid"`
	MemoryExternalID           string     `db:"memory_external_id"`
	Operation                  string     `db:"operation"`
	Path                       *string    `db:"path"`
	ContentSizeBytes           *int64     `db:"content_size_bytes"`
	ContentSHA256              *string    `db:"content_sha256"`
	S3Bucket                   *string    `db:"s3_bucket"`
	S3Key                      *string    `db:"s3_key"`
	CreatedByActorType         string     `db:"created_by_actor_type"`
	CreatedByAPIKeyUUID        *string    `db:"created_by_api_key_uuid"`
	CreatedByAPIKeyExternalID  *string    `db:"created_by_api_key_external_id"`
	CreatedBySessionID         *string    `db:"created_by_session_id"`
	CreatedByUserID            *string    `db:"created_by_user_id"`
	RedactedAt                 *time.Time `db:"redacted_at"`
	RedactedByActorType        *string    `db:"redacted_by_actor_type"`
	RedactedByAPIKeyUUID       *string    `db:"redacted_by_api_key_uuid"`
	RedactedByAPIKeyExternalID *string    `db:"redacted_by_api_key_external_id"`
	RedactedBySessionID        *string    `db:"redacted_by_session_id"`
	RedactedByUserID           *string    `db:"redacted_by_user_id"`
	CreatedAt                  time.Time  `db:"created_at"`
}

type memoryObjectRefRow struct {
	WorkspaceUUID string `db:"workspace_uuid"`
	Bucket        string `db:"bucket"`
	Key           string `db:"key"`
	ResourceID    string `db:"resource_id"`
}

type insertMemoryVersionParams struct {
	UUID                      string
	ExternalID                string
	OrganizationUUID          string
	WorkspaceUUID             string
	MemoryStoreUUID           string
	MemoryStoreExternalID     string
	MemoryUUID                string
	MemoryExternalID          string
	Operation                 string
	Path                      *string
	ContentSizeBytes          *int64
	ContentSHA256             *string
	S3Bucket                  *string
	S3Key                     *string
	CreatedByActorType        string
	CreatedByAPIKeyUUID       *string
	CreatedByAPIKeyExternalID *string
	CreatedBySessionID        *string
	CreatedByUserID           *string
	CreatedAt                 time.Time
}

type listMemoryVersionsParams struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	Limit                 int
	MemoryExternalID      string
	Operation             string
	APIKeyExternalID      string
	SessionID             string
	HasCreatedAtGTE       bool
	CreatedAtGTE          time.Time
	HasCreatedAtLTE       bool
	CreatedAtLTE          time.Time
	HasCursor             bool
	CursorCreatedAt       time.Time
	CursorUUID            string
}

type redactMemoryVersionParams struct {
	WorkspaceUUID              string
	MemoryStoreExternalID      string
	VersionExternalID          string
	RedactedAt                 time.Time
	RedactedByActorType        string
	RedactedByAPIKeyUUID       *string
	RedactedByAPIKeyExternalID *string
	RedactedBySessionID        *string
	RedactedByUserID           *string
}

type MemoryVersionMapper interface {
	Insert(ctx context.Context, params insertMemoryVersionParams) (memoryVersionRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, memoryStoreExternalID, versionExternalID string) (memoryVersionRow, error)
	FindForUpdate(ctx context.Context, workspaceUUID, memoryStoreExternalID, versionExternalID string) (memoryVersionRow, error)
	ListPage(ctx context.Context, params listMemoryVersionsParams) ([]memoryVersionRow, error)
	ListObjectRefsByStoreUUID(ctx context.Context, workspaceUUID, storeUUID string) ([]memoryObjectRefRow, error)
	RedactByExternalID(ctx context.Context, params redactMemoryVersionParams) (memoryVersionRow, error)
	DeleteByStoreUUID(ctx context.Context, workspaceUUID, storeUUID string) error
}
