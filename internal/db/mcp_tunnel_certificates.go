package db

import (
	"context"
	"time"
)

type MCPTunnelCertificate struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	TunnelUUID       string
	TunnelExternalID string
	CACertificatePEM string
	Fingerprint      string
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	ArchivedAt       *time.Time
}

type ListMCPTunnelCertificatesParams struct {
	OrganizationUUID string
	TunnelUUID       string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

func (d *DB) CreateMCPTunnelCertificate(
	ctx context.Context,
	certificate MCPTunnelCertificate,
) (MCPTunnelCertificate, error) {
	row, err := NewMCPTunnelCertificateMapper(d.mapperDB).Insert(ctx, insertMCPTunnelCertificateParams{
		UUID:             certificate.UUID,
		ExternalID:       certificate.ExternalID,
		OrganizationUUID: certificate.OrganizationUUID,
		TunnelUUID:       certificate.TunnelUUID,
		TunnelExternalID: certificate.TunnelExternalID,
		CACertificatePEM: certificate.CACertificatePEM,
		Fingerprint:      certificate.Fingerprint,
		ExpiresAt:        certificate.ExpiresAt,
		CreatedAt:        certificate.CreatedAt,
	})
	if isUniqueViolation(err) {
		return MCPTunnelCertificate{}, ErrDuplicate
	}
	if err != nil {
		return MCPTunnelCertificate{}, err
	}
	return mcpTunnelCertificateFromRow(row), nil
}

func (d *DB) GetMCPTunnelCertificate(
	ctx context.Context,
	organizationUUID string,
	tunnelUUID string,
	externalID string,
) (MCPTunnelCertificate, error) {
	row, err := NewMCPTunnelCertificateMapper(d.mapperDB).FindByExternalID(
		ctx,
		organizationUUID,
		tunnelUUID,
		externalID,
	)
	if err != nil {
		return MCPTunnelCertificate{}, mapNoRows(err)
	}
	return mcpTunnelCertificateFromRow(row), nil
}

func (d *DB) ListMCPTunnelCertificatesPage(
	ctx context.Context,
	params ListMCPTunnelCertificatesParams,
) ([]MCPTunnelCertificate, bool, error) {
	rows, err := NewMCPTunnelCertificateMapper(d.mapperDB).ListPage(ctx, listMCPTunnelCertificatesMapperParams{
		OrganizationUUID: params.OrganizationUUID,
		TunnelUUID:       params.TunnelUUID,
		IncludeArchived:  params.IncludeArchived,
		Limit:            params.Limit + 1,
		Offset:           params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	certificates := make([]MCPTunnelCertificate, len(rows))
	for index := range rows {
		certificates[index] = mcpTunnelCertificateFromRow(rows[index])
	}
	return trimAdminPage(certificates, params.Limit), len(certificates) > params.Limit, nil
}

func (d *DB) ArchiveMCPTunnelCertificate(
	ctx context.Context,
	organizationUUID string,
	tunnelUUID string,
	externalID string,
) (MCPTunnelCertificate, error) {
	row, err := NewMCPTunnelCertificateMapper(d.mapperDB).ArchiveByExternalID(
		ctx,
		organizationUUID,
		tunnelUUID,
		externalID,
	)
	if err != nil {
		return MCPTunnelCertificate{}, mapNoRows(err)
	}
	return mcpTunnelCertificateFromRow(row), nil
}

func mcpTunnelCertificateFromRow(row mcpTunnelCertificateRow) MCPTunnelCertificate {
	return MCPTunnelCertificate{
		UUID:             row.UUID,
		ExternalID:       row.ExternalID,
		OrganizationUUID: row.OrganizationUUID,
		TunnelUUID:       row.TunnelUUID,
		TunnelExternalID: row.TunnelExternalID,
		CACertificatePEM: row.CACertificatePEM,
		Fingerprint:      row.Fingerprint,
		ExpiresAt:        row.ExpiresAt,
		CreatedAt:        row.CreatedAt,
		ArchivedAt:       row.ArchivedAt,
	}
}
