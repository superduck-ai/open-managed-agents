package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper SessionEventMapper -sql ./session_event_mapper.xml -out ./session_event_mapper.sqlmap.gen.go -dialect postgres

type sessionEventRow struct {
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationUUID  string     `db:"organization_uuid"`
	WorkspaceUUID     string     `db:"workspace_uuid"`
	SessionUUID       string     `db:"session_uuid"`
	SessionExternalID string     `db:"session_external_id"`
	ThreadUUID        *string    `db:"thread_uuid"`
	ThreadExternalID  *string    `db:"thread_external_id"`
	EventType         string     `db:"event_type"`
	Payload           []byte     `db:"payload"`
	PayloadBlobUUID   *string    `db:"payload_blob_uuid"`
	ToolUseID         *string    `db:"tool_use_id"`
	ProcessedAt       *time.Time `db:"processed_at"`
	DeliverySeq       *int64     `db:"delivery_seq"`
	CreatedAt         time.Time  `db:"created_at"`
	DeletedAt         *time.Time `db:"deleted_at"`
}

type sessionEventWriteParams struct {
	UUID              string
	ExternalID        string
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionUUID       string
	SessionExternalID string
	ThreadUUID        *string
	ThreadExternalID  *string
	EventType         string
	Payload           []byte
	PayloadBlobUUID   *string
	ToolUseID         *string
	ProcessedAt       *time.Time
	CreatedAt         time.Time
}

type sessionEventPageMapperParams struct {
	WorkspaceUUID     string
	SessionExternalID string
	ThreadExternalID  string
	PrimaryOnly       bool
	FetchLimit        int
	Cursor            *SessionEventPageCursor
	Descending        bool
	Types             []string
	CreatedAtGT       *time.Time
	CreatedAtGTE      *time.Time
	CreatedAtLT       *time.Time
	CreatedAtLTE      *time.Time
}

type sessionEventStreamMapperParams struct {
	WorkspaceUUID     string
	SessionExternalID string
	ThreadExternalID  string
	PrimaryOnly       bool
	AfterSeq          int64
	FetchLimit        int
	EventExternalID   string
}

// SessionEventMapper contains queries whose primary table is session_events.
type SessionEventMapper interface {
	CursorExists(ctx context.Context, workspaceUUID, sessionExternalID, eventExternalID string) (bool, error)
	FindLatestStatus(ctx context.Context, workspaceUUID, sessionExternalID, threadID string) (sessionEventRow, bool, error)
	MarkProcessed(ctx context.Context, workspaceUUID, sessionID, eventID string, processedAt time.Time) (sessionEventRow, bool, error)
	Insert(ctx context.Context, params sessionEventWriteParams) (sessionEventRow, error)
	InsertIfAbsent(ctx context.Context, params sessionEventWriteParams) (sessionEventRow, bool, error)
	FindByExternalID(ctx context.Context, workspaceUUID, sessionExternalID, eventExternalID string) (sessionEventRow, error)
	FindAssistantEchoKeys(ctx context.Context, workspaceUUID, sessionExternalID, requestID, source string) ([]string, error)
	ListPage(ctx context.Context, params sessionEventPageMapperParams) ([]sessionEventRow, error)
	FindStreamPosition(ctx context.Context, params sessionEventStreamMapperParams) (int64, bool, error)
	LatestStreamPosition(ctx context.Context, params sessionEventStreamMapperParams) (int64, error)
	ListStreamPage(ctx context.Context, params sessionEventStreamMapperParams) ([]sessionEventRow, error)
	ChildSessionToolUseIDs(ctx context.Context, workspaceUUID, sessionExternalID string, eventTypes, toolUseIDs []string) ([]string, error)
	SoftDeleteBySession(ctx context.Context, workspaceUUID, sessionExternalID string) (int64, error)
	ListSessionEventsForActivation(
		ctx context.Context,
		organizationUUID string,
		workspaceUUID string,
		sessionUUID string,
	) ([]sessionEventRow, error)
}
