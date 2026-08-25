package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type AdminTunnel struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       *string
	WorkspaceExternalID *string
	DisplayName         *string
	Domain              string
	TokenID             *string
	TunnelToken         *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
}

type ListAdminTunnelsParams struct {
	OrganizationUUID    string
	WorkspaceExternalID string
	IncludeArchived     bool
	Limit               int
	Offset              int
}

func (d *DB) GetAdminTunnel(ctx context.Context, organizationUUID, externalID string) (AdminTunnel, error) {
	mapper := NewAdminMCPTunnelMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, organizationUUID, externalID)
	return adminTunnelFromMapperRow(row, err)
}

func (d *DB) ListAdminTunnelsPage(ctx context.Context, params ListAdminTunnelsParams) ([]AdminTunnel, bool, error) {
	mapper := NewAdminMCPTunnelMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, listAdminTunnelsMapperParams{
		OrganizationUUID:    params.OrganizationUUID,
		WorkspaceExternalID: params.WorkspaceExternalID,
		IncludeArchived:     params.IncludeArchived,
		Limit:               params.Limit + 1,
		Offset:              params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	tunnels, err := adminTunnelsFromMapperRows(rows)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(tunnels, params.Limit), len(tunnels) > params.Limit, nil
}

func (d *DB) SetAdminTunnelToken(ctx context.Context, organizationUUID, externalID, tokenID, token string) (AdminTunnel, error) {
	mapper := NewAdminMCPTunnelMapper(d.mapperDB)
	row, err := mapper.UpdateTokenByExternalID(ctx, updateAdminTunnelTokenParams{
		OrganizationUUID: organizationUUID,
		ExternalID:       externalID,
		TokenID:          tokenID,
		TunnelToken:      token,
	})
	return adminTunnelFromMapperRow(row, err)
}

func (d *DB) ArchiveAdminTunnel(ctx context.Context, organizationUUID, externalID string) (AdminTunnel, error) {
	var tunnel AdminTunnel
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		tunnelMapper := NewAdminMCPTunnelMapper(executor)
		certificateMapper := NewAdminMCPTunnelCertificateMapper(executor)
		row, txErr := tunnelMapper.ArchiveByExternalID(ctx, organizationUUID, externalID)
		tunnel, txErr = adminTunnelFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}
		return certificateMapper.ArchiveActiveByTunnelUUID(ctx, organizationUUID, row.UUID)
	})
	if err != nil {
		return AdminTunnel{}, err
	}
	return tunnel, nil
}

func adminTunnelFromMapperRow(row adminMCPTunnelRow, err error) (AdminTunnel, error) {
	if err != nil {
		return AdminTunnel{}, mapNoRows(err)
	}
	return AdminTunnel{
		UUID:                row.UUID,
		ExternalID:          row.ExternalID,
		OrganizationUUID:    row.OrganizationUUID,
		WorkspaceUUID:       row.WorkspaceUUID,
		WorkspaceExternalID: row.WorkspaceExternalID,
		DisplayName:         row.DisplayName,
		Domain:              row.Domain,
		TokenID:             row.TokenID,
		TunnelToken:         row.TunnelToken,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		ArchivedAt:          row.ArchivedAt,
	}, nil
}

func adminTunnelsFromMapperRows(rows []adminMCPTunnelRow) ([]AdminTunnel, error) {
	tunnels := make([]AdminTunnel, len(rows))
	for index := range rows {
		tunnel, err := adminTunnelFromMapperRow(rows[index], nil)
		if err != nil {
			return nil, err
		}
		tunnels[index] = tunnel
	}
	return tunnels, nil
}
