package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminTunnelCertificate struct {
	UUID             uuid.UUID
	ExternalID       string
	OrganizationUUID uuid.UUID
	TunnelUUID       uuid.UUID
	TunnelExternalID string
	CACertificatePEM string
	Fingerprint      string
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	ArchivedAt       *time.Time
}

type ListAdminTunnelCertificatesParams struct {
	OrganizationUUID string
	TunnelUUID       string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

func (d *DB) CreateAdminTunnelCertificate(ctx context.Context, cert AdminTunnelCertificate) (AdminTunnelCertificate, error) {
	mapper := NewAdminMCPTunnelCertificateMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, insertAdminTunnelCertificateParams{
		ExternalID:       cert.ExternalID,
		OrganizationUUID: cert.OrganizationUUID.String(),
		TunnelUUID:       cert.TunnelUUID.String(),
		TunnelExternalID: cert.TunnelExternalID,
		CACertificatePEM: cert.CACertificatePEM,
		Fingerprint:      cert.Fingerprint,
		ExpiresAt:        cert.ExpiresAt,
		CreatedAt:        cert.CreatedAt,
	})
	if isUniqueViolation(err) {
		return AdminTunnelCertificate{}, ErrDuplicate
	}
	return adminTunnelCertificateFromMapperRow(row, err)
}

func (d *DB) GetAdminTunnelCertificate(ctx context.Context, organizationUUID, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	mapper := NewAdminMCPTunnelCertificateMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, organizationUUID, tunnelExternalID, certExternalID)
	return adminTunnelCertificateFromMapperRow(row, err)
}

func (d *DB) ListAdminTunnelCertificatesPage(ctx context.Context, params ListAdminTunnelCertificatesParams) ([]AdminTunnelCertificate, bool, error) {
	mapper := NewAdminMCPTunnelCertificateMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, listAdminTunnelCertificatesMapperParams{
		OrganizationUUID: params.OrganizationUUID,
		TunnelUUID:       params.TunnelUUID,
		IncludeArchived:  params.IncludeArchived,
		Limit:            params.Limit + 1,
		Offset:           params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	certs, err := adminTunnelCertificatesFromMapperRows(rows)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(certs, params.Limit), len(certs) > params.Limit, nil
}

func (d *DB) ArchiveAdminTunnelCertificate(ctx context.Context, organizationUUID, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	mapper := NewAdminMCPTunnelCertificateMapper(d.mapperDB)
	row, err := mapper.ArchiveByExternalID(ctx, organizationUUID, tunnelExternalID, certExternalID)
	return adminTunnelCertificateFromMapperRow(row, err)
}

func (d *DB) CountActiveAdminTunnelCertificates(ctx context.Context, organizationUUID, tunnelUUID string) (int, error) {
	mapper := NewAdminMCPTunnelCertificateMapper(d.mapperDB)
	return mapper.CountActive(ctx, organizationUUID, tunnelUUID)
}

func adminTunnelCertificateFromMapperRow(row adminMCPTunnelCertificateRow, err error) (AdminTunnelCertificate, error) {
	if err != nil {
		return AdminTunnelCertificate{}, mapNoRows(err)
	}
	certificateUUID, err := uuid.Parse(row.UUID)
	if err != nil {
		return AdminTunnelCertificate{}, err
	}
	organizationUUID, err := uuid.Parse(row.OrganizationUUID)
	if err != nil {
		return AdminTunnelCertificate{}, err
	}
	tunnelUUID, err := uuid.Parse(row.TunnelUUID)
	if err != nil {
		return AdminTunnelCertificate{}, err
	}
	return AdminTunnelCertificate{
		UUID:             certificateUUID,
		ExternalID:       row.ExternalID,
		OrganizationUUID: organizationUUID,
		TunnelUUID:       tunnelUUID,
		TunnelExternalID: row.TunnelExternalID,
		CACertificatePEM: row.CACertificatePEM,
		Fingerprint:      row.Fingerprint,
		ExpiresAt:        row.ExpiresAt,
		CreatedAt:        row.CreatedAt,
		ArchivedAt:       row.ArchivedAt,
	}, nil
}

func adminTunnelCertificatesFromMapperRows(rows []adminMCPTunnelCertificateRow) ([]AdminTunnelCertificate, error) {
	certificates := make([]AdminTunnelCertificate, len(rows))
	for index := range rows {
		certificate, err := adminTunnelCertificateFromMapperRow(rows[index], nil)
		if err != nil {
			return nil, err
		}
		certificates[index] = certificate
	}
	return certificates, nil
}
