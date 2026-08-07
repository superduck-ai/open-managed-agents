package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionThreadMapper -sql ./session_thread_mapper.xml -out ./session_thread_mapper.sqlmap.gen.go -dialect postgres

type sessionThreadRow struct {
	UUID                   string     `db:"uuid"`
	ExternalID             string     `db:"external_id"`
	OrganizationUUID       string     `db:"organization_uuid"`
	WorkspaceUUID          string     `db:"workspace_uuid"`
	SessionUUID            string     `db:"session_uuid"`
	SessionExternalID      string     `db:"session_external_id"`
	ParentThreadUUID       *string    `db:"parent_thread_uuid"`
	ParentThreadExternalID *string    `db:"parent_thread_external_id"`
	AgentSnapshot          []byte     `db:"agent_snapshot"`
	Status                 string     `db:"status"`
	Usage                  []byte     `db:"usage"`
	Stats                  []byte     `db:"stats"`
	CreatedAt              time.Time  `db:"created_at"`
	UpdatedAt              time.Time  `db:"updated_at"`
	ArchivedAt             *time.Time `db:"archived_at"`
	DeletedAt              *time.Time `db:"deleted_at"`
}

type sessionThreadWriteParams struct {
	UUID                   string
	ExternalID             string
	OrganizationUUID       string
	WorkspaceUUID          string
	SessionUUID            string
	SessionExternalID      string
	ParentThreadUUID       *string
	ParentThreadExternalID *string
	AgentSnapshot          []byte
	Status                 string
	Usage                  []byte
	Stats                  []byte
	CreatedAt              time.Time
}

type sessionThreadPageMapperParams struct {
	WorkspaceUUID     string
	SessionExternalID string
	FetchLimit        int
	Cursor            *SessionThreadPageCursor
}

// SessionThreadMapper contains queries whose primary table is session_threads.
type SessionThreadMapper interface {
	Insert(ctx context.Context, params sessionThreadWriteParams) (sessionThreadRow, error)
	InsertIfAbsent(ctx context.Context, params sessionThreadWriteParams) (sessionThreadRow, bool, error)
	FindPrimary(ctx context.Context, workspaceUUID, sessionExternalID string) (sessionThreadRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, sessionExternalID, threadExternalID string) (sessionThreadRow, error)
	ListPage(ctx context.Context, params sessionThreadPageMapperParams) ([]sessionThreadRow, error)
	List(ctx context.Context, workspaceUUID, sessionExternalID string) ([]sessionThreadRow, error)
	SetStatus(ctx context.Context, workspaceUUID, sessionExternalID, threadExternalID, status string) (int64, error)
	Archive(ctx context.Context, workspaceUUID, sessionExternalID, threadExternalID string) (sessionThreadRow, error)
	SoftDeleteBySession(ctx context.Context, workspaceUUID, sessionExternalID string) (int64, error)
}
