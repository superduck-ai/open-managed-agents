package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminTunnelCertificate struct {
	UUID             uuid.UUID  `db:"uuid"`
	ExternalID       string     `db:"external_id"`
	OrganizationUUID uuid.UUID  `db:"organization_uuid"`
	TunnelUUID       uuid.UUID  `db:"tunnel_uuid"`
	TunnelExternalID string     `db:"tunnel_external_id"`
	CACertificatePEM string     `db:"ca_certificate_pem"`
	Fingerprint      string     `db:"fingerprint"`
	ExpiresAt        *time.Time `db:"expires_at"`
	CreatedAt        time.Time  `db:"created_at"`
	ArchivedAt       *time.Time `db:"archived_at"`
}

type ListAdminTunnelCertificatesParams struct {
	OrganizationUUID string
	TunnelUUID       string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

func (d *DB) CreateAdminTunnelCertificate(ctx context.Context, cert AdminTunnelCertificate) (AdminTunnelCertificate, error) {
	created, err := getAdminRow[AdminTunnelCertificate](ctx, d.sql, `
		insert into mcp_tunnel_certificates (
			external_id, organization_uuid, tunnel_uuid, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at
		)
		values (
			:external_id, :organization_uuid, :tunnel_uuid, :tunnel_external_id,
			:ca_certificate_pem, :fingerprint, :expires_at, :created_at
		)
		returning uuid, external_id,
			organization_uuid,
			tunnel_uuid, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, adminTunnelCertificateArguments(cert))
	if isUniqueViolation(err) {
		return AdminTunnelCertificate{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminTunnelCertificate(ctx context.Context, organizationUUID, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	return getAdminRow[AdminTunnelCertificate](ctx, d.sql, adminTunnelCertificateSelectSQL()+`
		where organization_uuid = :organization_uuid
			and tunnel_external_id = :tunnel_external_id
			and external_id = :external_id
	`, map[string]any{
		"organization_uuid":  dbUUID(organizationUUID),
		"tunnel_external_id": tunnelExternalID,
		"external_id":        certExternalID,
	})
}

func (d *DB) ListAdminTunnelCertificatesPage(ctx context.Context, params ListAdminTunnelCertificatesParams) ([]AdminTunnelCertificate, bool, error) {
	query := adminTunnelCertificateSelectSQL() + `
		where organization_uuid = :organization_uuid
			and tunnel_uuid = :tunnel_uuid
	`
	args := map[string]any{
		"organization_uuid": dbUUID(params.OrganizationUUID),
		"tunnel_uuid":       dbUUID(params.TunnelUUID),
		"limit":             params.Limit + 1,
		"offset":            params.Offset,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	query += " order by created_at desc, uuid desc limit :limit offset :offset"
	certs, err := selectAdminRows[AdminTunnelCertificate](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(certs, params.Limit), len(certs) > params.Limit, nil
}

func (d *DB) ArchiveAdminTunnelCertificate(ctx context.Context, organizationUUID, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	return getAdminRow[AdminTunnelCertificate](ctx, d.sql, `
		update mcp_tunnel_certificates
		set archived_at = coalesce(archived_at, now())
		where organization_uuid = :organization_uuid
			and tunnel_external_id = :tunnel_external_id
			and external_id = :external_id
		returning uuid, external_id,
			organization_uuid,
			tunnel_uuid, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, map[string]any{
		"organization_uuid":  dbUUID(organizationUUID),
		"tunnel_external_id": tunnelExternalID,
		"external_id":        certExternalID,
	})
}

func (d *DB) CountActiveAdminTunnelCertificates(ctx context.Context, organizationUUID, tunnelUUID string) (int, error) {
	var count int
	err := namedGetContext(ctx, d.sql, &count, `
		select count(*)
		from mcp_tunnel_certificates
		where organization_uuid = :organization_uuid
			and tunnel_uuid = :tunnel_uuid
			and archived_at is null
	`, map[string]any{
		"organization_uuid": dbUUID(organizationUUID),
		"tunnel_uuid":       dbUUID(tunnelUUID),
	})
	return count, err
}

func adminTunnelCertificateSelectSQL() string {
	return `
		select uuid, external_id,
			organization_uuid,
			tunnel_uuid, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
		from mcp_tunnel_certificates
	`
}

func adminTunnelCertificateArguments(cert AdminTunnelCertificate) map[string]any {
	return map[string]any{
		"external_id":        cert.ExternalID,
		"organization_uuid":  cert.OrganizationUUID,
		"tunnel_uuid":        cert.TunnelUUID,
		"tunnel_external_id": cert.TunnelExternalID,
		"ca_certificate_pem": cert.CACertificatePEM,
		"fingerprint":        cert.Fingerprint,
		"expires_at":         cert.ExpiresAt,
		"created_at":         cert.CreatedAt,
	}
}
