package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminMCPTunnelMapper -sql ./admin_mcp_tunnel_mapper.xml -out ./admin_mcp_tunnel_mapper.sqlmap.gen.go -dialect postgres

type adminMCPTunnelRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       *string    `db:"workspace_uuid"`
	WorkspaceExternalID *string    `db:"workspace_external_id"`
	DisplayName         *string    `db:"display_name"`
	Domain              string     `db:"domain"`
	TokenID             *string    `db:"token_id"`
	TunnelToken         *string    `db:"tunnel_token"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
}

type listAdminTunnelsMapperParams struct {
	OrganizationUUID    string
	WorkspaceExternalID string
	IncludeArchived     bool
	Limit               int
	Offset              int
}

type updateAdminTunnelTokenParams struct {
	OrganizationUUID string
	ExternalID       string
	TokenID          string
	TunnelToken      string
}

type AdminMCPTunnelMapper interface {
	FindByExternalID(ctx context.Context, organizationUUID, externalID string) (adminMCPTunnelRow, error)
	ListPage(ctx context.Context, params listAdminTunnelsMapperParams) ([]adminMCPTunnelRow, error)
	UpdateTokenByExternalID(ctx context.Context, params updateAdminTunnelTokenParams) (adminMCPTunnelRow, error)
	ArchiveByExternalID(ctx context.Context, organizationUUID, externalID string) (adminMCPTunnelRow, error)
}
