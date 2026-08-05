package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AgentMapper -sql ./agents_mapper.xml -out ./agents_mapper.sqlmap.gen.go -dialect postgres

type agentRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID *string    `db:"created_by_api_key_uuid"`
	CurrentVersion      int        `db:"current_version"`
	Name                string     `db:"name"`
	Description         *string    `db:"description"`
	System              *string    `db:"system"`
	Model               []byte     `db:"model"`
	MCPServers          []byte     `db:"mcp_servers"`
	Metadata            []byte     `db:"metadata"`
	Multiagent          []byte     `db:"multiagent"`
	Skills              []byte     `db:"skills"`
	Tools               []byte     `db:"tools"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type agentConfigParams struct {
	Name        string
	Description *string
	System      *string
	Model       []byte
	MCPServers  []byte
	Metadata    []byte
	Multiagent  []byte
	Skills      []byte
	Tools       []byte
}

type insertAgentParams struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	Config              agentConfigParams
	CreatedAt           time.Time
}

type updateAgentParams struct {
	WorkspaceUUID  string
	ExternalID     string
	CurrentVersion int
	Config         agentConfigParams
	UpdatedAt      time.Time
}

type insertAgentVersionParams struct {
	ExternalID      string
	WorkspaceUUID   string
	AgentUUID       string
	AgentExternalID string
	Version         int
	Config          agentConfigParams
	AgentCreatedAt  time.Time
	AgentUpdatedAt  time.Time
	ArchivedAt      *time.Time
}

type agentPageFilter struct {
	WorkspaceUUID   string
	Name            string
	Limit           int
	Cursor          *AgentPageCursor
	IncludeArchived bool
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type AgentMapper interface {
	Insert(ctx context.Context, params insertAgentParams) (agentRow, error)
	InsertVersion(ctx context.Context, params insertAgentVersionParams) error
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (agentRow, error)
	FindVersion(ctx context.Context, workspaceUUID, externalID string, version int) (agentRow, error)
	LockByExternalID(ctx context.Context, workspaceUUID, externalID string) (agentRow, error)
	UpdateByExternalID(ctx context.Context, params updateAgentParams) (agentRow, error)
	ArchiveByExternalID(ctx context.Context, workspaceUUID, externalID string) (agentRow, error)
	ListPage(ctx context.Context, filter agentPageFilter) ([]agentRow, error)
	FindUUIDByExternalID(ctx context.Context, workspaceUUID, externalID string) (string, error)
	ListVersionsPage(ctx context.Context, agentUUID string, cursor *AgentVersionPageCursor, limit int) ([]agentRow, error)
}
