package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MCPTunnelMapper -sql ./mcp_tunnel.xml -out ./mcp_tunnel.sqlmap.gen.go -dialect postgres

type mcpTunnelRow struct {
	UUID             string     `db:"uuid"`
	ExternalID       string     `db:"external_id"`
	OrganizationUUID string     `db:"organization_uuid"`
	WorkspaceUUID    string     `db:"workspace_uuid"`
	DisplayName      *string    `db:"display_name"`
	Domain           string     `db:"domain"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
	ArchivedAt       *time.Time `db:"archived_at"`
}

type insertMCPTunnelParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	DisplayName      *string
	Domain           string
	CreatedAt        time.Time
}

type listMCPTunnelsMapperParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

type MCPTunnelMapper interface {
	Insert(ctx context.Context, params insertMCPTunnelParams) (mcpTunnelRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (mcpTunnelRow, error)
	FindByDomain(ctx context.Context, organizationUUID, workspaceUUID, domain string) (mcpTunnelRow, error)
	FindActiveForUpdate(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (mcpTunnelRow, error)
	ListPage(ctx context.Context, params listMCPTunnelsMapperParams) ([]mcpTunnelRow, error)
	ArchiveByExternalID(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (mcpTunnelRow, error)
}
