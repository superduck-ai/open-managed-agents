package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MCPServerMapper -sql ./mcp_server_mapper.xml -out ./mcp_server_mapper.sqlmap.gen.go -dialect postgres

type mcpServerMapperRow struct {
	UUID             string     `db:"uuid"`
	ExternalID       string     `db:"external_id"`
	OrganizationUUID string     `db:"organization_uuid"`
	WorkspaceUUID    string     `db:"workspace_uuid"`
	Name             string     `db:"name"`
	TransportType    string     `db:"transport_type"`
	EndpointURL      string     `db:"endpoint_url"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
	DeletedAt        *time.Time `db:"deleted_at"`
}

type mcpServerWriteParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	Name             string
	TransportType    string
	EndpointURL      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type mcpServerPageMapperParams struct {
	WorkspaceUUID string
	Search        string
	FetchLimit    int
	Cursor        *WorkspaceMCPServerPageCursor
}

type MCPServerMapper interface {
	Insert(ctx context.Context, params mcpServerWriteParams) (mcpServerMapperRow, error)
	FindByExternalID(ctx context.Context, workspaceUUID, externalID string) (mcpServerMapperRow, error)
	UpdateByExternalID(ctx context.Context, params mcpServerWriteParams) (mcpServerMapperRow, error)
	SoftDeleteByExternalID(ctx context.Context, workspaceUUID, externalID string) (mcpServerMapperRow, error)
	ListPage(ctx context.Context, params mcpServerPageMapperParams) ([]mcpServerMapperRow, error)
}
