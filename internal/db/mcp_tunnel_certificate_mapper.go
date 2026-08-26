package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MCPTunnelCertificateMapper -sql ./mcp_tunnel_certificate.xml -out ./mcp_tunnel_certificate.sqlmap.gen.go -dialect postgres

type MCPTunnelCertificateMapper interface {
	Insert(ctx context.Context, params insertMCPTunnelCertificateParams) (mcpTunnelCertificateRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, tunnelUUID, externalID string) (mcpTunnelCertificateRow, error)
	ListPage(ctx context.Context, params listMCPTunnelCertificatesMapperParams) ([]mcpTunnelCertificateRow, error)
	ArchiveByExternalID(ctx context.Context, organizationUUID, tunnelUUID, externalID string) (mcpTunnelCertificateRow, error)
}

type mcpTunnelCertificateRow struct {
	UUID             string     `db:"uuid"`
	ExternalID       string     `db:"external_id"`
	OrganizationUUID string     `db:"organization_uuid"`
	TunnelUUID       string     `db:"tunnel_uuid"`
	TunnelExternalID string     `db:"tunnel_external_id"`
	CACertificatePEM string     `db:"ca_certificate_pem"`
	Fingerprint      string     `db:"fingerprint"`
	ExpiresAt        *time.Time `db:"expires_at"`
	CreatedAt        time.Time  `db:"created_at"`
	ArchivedAt       *time.Time `db:"archived_at"`
}

type insertMCPTunnelCertificateParams struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	TunnelUUID       string
	TunnelExternalID string
	CACertificatePEM string
	Fingerprint      string
	ExpiresAt        *time.Time
	CreatedAt        time.Time
}

type listMCPTunnelCertificatesMapperParams struct {
	OrganizationUUID string
	TunnelUUID       string
	IncludeArchived  bool
	Limit            int
	Offset           int
}
