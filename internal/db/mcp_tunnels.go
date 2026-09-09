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
	if err != nil {
		return MCPTunnelTokenVersion{}, err
	}
	if tunnel.ArchivedAt != nil {
		return MCPTunnelTokenVersion{}, ErrNotFound
	}
	row, found, err := NewMCPTunnelTokenMapper(d.mapperDB).FindActiveByTunnelUUID(ctx, tunnel.UUID)
	if err != nil {
		return MCPTunnelTokenVersion{}, err
	}
	if !found {
		return MCPTunnelTokenVersion{}, ErrNotFound
	}
	return mcpTunnelTokenFromRow(row), nil
}

// MCPTunnelTokenTx is valid only inside WithMCPTunnelTokenTx's synchronous callback.
// The resource row lock serializes token mutations with control-state recovery.
type MCPTunnelTokenTx struct {
	executor yourbatis.Executor
	Tunnel   MCPTunnel
	Token    MCPTunnelTokenVersion
}

func (d *DB) WithMCPTunnelTokenTx(ctx context.Context, organizationUUID, workspaceUUID, externalID string, fn func(*MCPTunnelTokenTx) error) error {
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, err := NewMCPTunnelMapper(executor).FindActiveForUpdate(ctx, organizationUUID, workspaceUUID, externalID)
		if err != nil {
			return mapNoRows(err)
		}
		token, found, err := NewMCPTunnelTokenMapper(executor).FindActiveForUpdate(ctx, row.UUID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		return fn(&MCPTunnelTokenTx{executor: executor, Tunnel: mcpTunnelFromRow(row), Token: mcpTunnelTokenFromRow(token)})
	})
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
}

func (tx *MCPTunnelTokenTx) Rotate(ctx context.Context, expectedVersion int64, next MCPTunnelTokenVersion) (MCPTunnelTokenVersion, error) {
	if next.Envelope == nil {
		return MCPTunnelTokenVersion{}, ErrIncompleteSecretEnvelope
	}
	nextVersion, err := nextMCPTunnelTokenVersion(tx.Token.Version, expectedVersion)
	if err != nil {
		return MCPTunnelTokenVersion{}, err
	}
	mapper := NewMCPTunnelTokenMapper(tx.executor)
	rows, err := mapper.RetireActiveByTunnelUUID(ctx, tx.Tunnel.UUID, next.CreatedAt)
	if err != nil {
		return MCPTunnelTokenVersion{}, err
	}
	if rows != 1 {
		return MCPTunnelTokenVersion{}, ErrInvalidState
	}
	next.TunnelUUID, next.Version = tx.Tunnel.UUID, nextVersion
	row, err := mapper.Insert(ctx, mcpTunnelTokenInsertParams(next))
	if err != nil {
		return MCPTunnelTokenVersion{}, err
	}
	return mcpTunnelTokenFromRow(row), nil
}

func nextMCPTunnelTokenVersion(currentVersion, expectedVersion int64) (int64, error) {
	if currentVersion != expectedVersion {
		return 0, ErrInvalidState
	}
	return currentVersion + 1, nil
}

func (tx *MCPTunnelTokenTx) Archive(ctx context.Context) (MCPTunnel, error) {
	row, err := NewMCPTunnelMapper(tx.executor).ArchiveByExternalID(ctx, tx.Tunnel.OrganizationUUID, tx.Tunnel.WorkspaceUUID, tx.Tunnel.ExternalID)
	if err != nil {
		return MCPTunnel{}, mapNoRows(err)
	}
	archivedAt := time.Now().UTC()
	if row.ArchivedAt != nil {
		archivedAt = *row.ArchivedAt
	}
	if err := NewMCPTunnelTokenMapper(tx.executor).ArchiveByTunnelUUID(ctx, row.UUID, archivedAt); err != nil {
		return MCPTunnel{}, err
	}
	return mcpTunnelFromRow(row), nil
}

func (d *DB) FindMCPTunnelTokenContext(ctx context.Context, tunnelExternalID string, tokenHash []byte) (MCPTunnelTokenContext, error) {
	row, found, err := NewMCPTunnelTokenMapper(d.mapperDB).FindByHashAndTunnelExternalID(ctx, tokenHash, tunnelExternalID)
	if err != nil {
		return MCPTunnelTokenContext{}, err
	}
	if !found {
		return MCPTunnelTokenContext{}, ErrNotFound
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
