package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminMCPTunnelCertificateMapper -sql ./admin_mcp_tunnel_certificate_mapper.xml -out ./admin_mcp_tunnel_certificate_mapper.sqlmap.gen.go -dialect postgres

type adminMCPTunnelCertificateRow struct {
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

type insertAdminTunnelCertificateParams struct {
	ExternalID       string
	OrganizationUUID string
	TunnelUUID       string
	TunnelExternalID string
	CACertificatePEM string
	Fingerprint      string
	ExpiresAt        *time.Time
	CreatedAt        time.Time
}

type listAdminTunnelCertificatesMapperParams struct {
	OrganizationUUID string
	TunnelUUID       string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

type AdminMCPTunnelCertificateMapper interface {
	Insert(ctx context.Context, params insertAdminTunnelCertificateParams) (adminMCPTunnelCertificateRow, error)
	FindByExternalID(ctx context.Context, organizationUUID, tunnelExternalID, certificateExternalID string) (adminMCPTunnelCertificateRow, error)
	ListPage(ctx context.Context, params listAdminTunnelCertificatesMapperParams) ([]adminMCPTunnelCertificateRow, error)
	ArchiveByExternalID(ctx context.Context, organizationUUID, tunnelExternalID, certificateExternalID string) (adminMCPTunnelCertificateRow, error)
	CountActive(ctx context.Context, organizationUUID, tunnelUUID string) (int, error)
	ArchiveActiveByTunnelUUID(ctx context.Context, organizationUUID, tunnelUUID string) error
}
