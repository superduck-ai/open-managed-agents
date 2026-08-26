package db

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/yourbatis"
)

type MCPTunnel struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	DisplayName      *string
	Domain           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ArchivedAt       *time.Time
}

type MCPTunnelTokenVersion struct {
	UUID       string
	ExternalID string
	TunnelUUID string
	Version    int64
	TokenHash  []byte
	Envelope   *secrets.Envelope
	CreatedAt  time.Time
	RetiredAt  *time.Time
	ArchivedAt *time.Time
}

type MCPTunnelTokenContext struct {
	Token            MCPTunnelTokenVersion
	TunnelExternalID string
	OrganizationUUID string
	WorkspaceUUID    string
	TunnelArchivedAt *time.Time
}

type ListMCPTunnelsParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

func (d *DB) CreateMCPTunnel(ctx context.Context, tunnel MCPTunnel, token MCPTunnelTokenVersion) (MCPTunnel, error) {
	if token.Envelope == nil {
		return MCPTunnel{}, ErrIncompleteSecretEnvelope
	}
	var created MCPTunnel
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		tunnelMapper := NewMCPTunnelMapper(executor)
		row, err := tunnelMapper.Insert(ctx, insertMCPTunnelParams{
			UUID:             tunnel.UUID,
			ExternalID:       tunnel.ExternalID,
			OrganizationUUID: tunnel.OrganizationUUID,
			WorkspaceUUID:    tunnel.WorkspaceUUID,
			DisplayName:      tunnel.DisplayName,
			Domain:           tunnel.Domain,
			CreatedAt:        tunnel.CreatedAt,
		})
		if err != nil {
			return err
		}
		created = mcpTunnelFromRow(row)
		token.TunnelUUID = row.UUID
		if token.Version == 0 {
			token.Version = 1
		}
		_, err = NewMCPTunnelTokenMapper(executor).Insert(ctx, mcpTunnelTokenInsertParams(token))
		return err
	})
	if isUniqueViolation(err) {
		return MCPTunnel{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetMCPTunnel(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (MCPTunnel, error) {
	row, err := NewMCPTunnelMapper(d.mapperDB).FindByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil {
		return MCPTunnel{}, mapNoRows(err)
	}
	return mcpTunnelFromRow(row), nil
}

func (d *DB) GetMCPTunnelByDomain(ctx context.Context, organizationUUID, workspaceUUID, domain string) (MCPTunnel, error) {
	row, err := NewMCPTunnelMapper(d.mapperDB).FindByDomain(ctx, organizationUUID, workspaceUUID, domain)
	if err != nil {
		return MCPTunnel{}, mapNoRows(err)
	}
	return mcpTunnelFromRow(row), nil
}

func (d *DB) ListMCPTunnelsPage(ctx context.Context, params ListMCPTunnelsParams) ([]MCPTunnel, bool, error) {
	rows, err := NewMCPTunnelMapper(d.mapperDB).ListPage(ctx, listMCPTunnelsMapperParams{
		OrganizationUUID: params.OrganizationUUID,
		WorkspaceUUID:    params.WorkspaceUUID,
		IncludeArchived:  params.IncludeArchived,
		Limit:            params.Limit + 1,
		Offset:           params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	tunnels := make([]MCPTunnel, len(rows))
	for index := range rows {
		tunnels[index] = mcpTunnelFromRow(rows[index])
	}
	return trimAdminPage(tunnels, params.Limit), len(tunnels) > params.Limit, nil
}

func (d *DB) GetActiveMCPTunnelToken(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (MCPTunnelTokenVersion, error) {
	tunnel, err := d.GetMCPTunnel(ctx, organizationUUID, workspaceUUID, externalID)
	if err != nil || tunnel.ArchivedAt != nil {
		return MCPTunnelTokenVersion{}, ErrNotFound
	}
	row, err := NewMCPTunnelTokenMapper(d.mapperDB).FindActiveByTunnelUUID(ctx, tunnel.UUID)
	if err != nil {
		return MCPTunnelTokenVersion{}, mapNoRows(err)
	}
	return mcpTunnelTokenFromRow(row), nil
}

func (d *DB) RotateMCPTunnelToken(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	externalID string,
	expectedVersion int64,
	next MCPTunnelTokenVersion,
) (MCPTunnelTokenVersion, error) {
	if next.Envelope == nil {
		return MCPTunnelTokenVersion{}, ErrIncompleteSecretEnvelope
	}
	var created MCPTunnelTokenVersion
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		tunnelRow, err := NewMCPTunnelMapper(executor).FindActiveForUpdate(ctx, organizationUUID, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		tokenMapper := NewMCPTunnelTokenMapper(executor)
		current, err := tokenMapper.FindActiveForUpdate(ctx, tunnelRow.UUID)
		if err != nil {
			return mapNoRows(err)
		}
		nextVersion, err := nextMCPTunnelTokenVersion(current.Version, expectedVersion)
		if err != nil {
			return err
		}
		now := next.CreatedAt
		rows, err := tokenMapper.RetireActiveByTunnelUUID(ctx, tunnelRow.UUID, now)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrInvalidState
		}
		next.TunnelUUID = tunnelRow.UUID
		next.Version = nextVersion
		row, err := tokenMapper.Insert(ctx, mcpTunnelTokenInsertParams(next))
		if err != nil {
			return err
		}
		created = mcpTunnelTokenFromRow(row)
		return nil
	})
	if isUniqueViolation(err) {
		return MCPTunnelTokenVersion{}, ErrDuplicate
	}
	return created, err
}

func nextMCPTunnelTokenVersion(currentVersion, expectedVersion int64) (int64, error) {
	if currentVersion != expectedVersion {
		return 0, ErrInvalidState
	}
	return currentVersion + 1, nil
}

func (d *DB) ArchiveMCPTunnel(ctx context.Context, organizationUUID, workspaceUUID, externalID string) (MCPTunnel, error) {
	var archived MCPTunnel
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		tunnelMapper := NewMCPTunnelMapper(executor)
		row, err := tunnelMapper.ArchiveByExternalID(ctx, organizationUUID, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		archived = mcpTunnelFromRow(row)
		archivedAt := time.Now().UTC()
		if row.ArchivedAt != nil {
			archivedAt = *row.ArchivedAt
		}
		if err := NewMCPTunnelTokenMapper(executor).ArchiveByTunnelUUID(ctx, row.UUID, archivedAt); err != nil {
			return err
		}
		return nil
	})
	return archived, err
}

func (d *DB) FindMCPTunnelTokenContext(ctx context.Context, tunnelExternalID string, tokenHash []byte) (MCPTunnelTokenContext, error) {
	row, err := NewMCPTunnelTokenMapper(d.mapperDB).FindByHashAndTunnelExternalID(ctx, tokenHash, tunnelExternalID)
	if err != nil {
		return MCPTunnelTokenContext{}, mapNoRows(err)
	}
	tokenRow := mcpTunnelTokenRow{
		UUID: row.UUID, ExternalID: row.ExternalID, TunnelUUID: row.TunnelUUID,
		Version: row.Version, TokenHash: row.TokenHash, Ciphertext: row.Ciphertext,
		Nonce: row.Nonce, WrappedDEK: row.WrappedDEK, FormatVersion: row.FormatVersion,
		KeyProvider: row.KeyProvider, KeyVersion: row.KeyVersion, CreatedAt: row.CreatedAt,
		RetiredAt: row.RetiredAt, ArchivedAt: row.ArchivedAt,
	}
	return MCPTunnelTokenContext{
		Token:            mcpTunnelTokenFromRow(tokenRow),
		TunnelExternalID: row.TunnelExternalID,
		OrganizationUUID: row.OrganizationUUID,
		WorkspaceUUID:    row.WorkspaceUUID,
		TunnelArchivedAt: row.TunnelArchivedAt,
	}, nil
}

func mcpTunnelFromRow(row mcpTunnelRow) MCPTunnel {
	return MCPTunnel{
		UUID: row.UUID, ExternalID: row.ExternalID, OrganizationUUID: row.OrganizationUUID,
		WorkspaceUUID: row.WorkspaceUUID, DisplayName: row.DisplayName, Domain: row.Domain,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ArchivedAt: row.ArchivedAt,
	}
}

func mcpTunnelTokenInsertParams(token MCPTunnelTokenVersion) insertMCPTunnelTokenParams {
	envelope := token.Envelope
	return insertMCPTunnelTokenParams{
		UUID: token.UUID, ExternalID: token.ExternalID, TunnelUUID: token.TunnelUUID,
		Version: token.Version, TokenHash: token.TokenHash, Ciphertext: envelope.Ciphertext,
		Nonce: envelope.Nonce, WrappedDEK: envelope.WrappedDEK,
		FormatVersion: envelope.FormatVersion, KeyProvider: envelope.KeyProvider,
		KeyVersion: envelope.KeyVersion, CreatedAt: token.CreatedAt,
	}
}

func mcpTunnelTokenFromRow(row mcpTunnelTokenRow) MCPTunnelTokenVersion {
	var envelope *secrets.Envelope
	if len(row.Ciphertext) > 0 && len(row.Nonce) > 0 && len(row.WrappedDEK) > 0 &&
		row.FormatVersion.Valid && row.KeyProvider.Valid && row.KeyVersion.Valid {
		envelope = &secrets.Envelope{
			Ciphertext: row.Ciphertext, Nonce: row.Nonce, WrappedDEK: row.WrappedDEK,
			FormatVersion: int(row.FormatVersion.Int32), KeyProvider: row.KeyProvider.String,
			KeyVersion: row.KeyVersion.Int64,
		}
	}
	return MCPTunnelTokenVersion{
		UUID: row.UUID, ExternalID: row.ExternalID, TunnelUUID: row.TunnelUUID,
		Version: row.Version, TokenHash: row.TokenHash, Envelope: envelope,
		CreatedAt: row.CreatedAt, RetiredAt: row.RetiredAt, ArchivedAt: row.ArchivedAt,
	}
}
