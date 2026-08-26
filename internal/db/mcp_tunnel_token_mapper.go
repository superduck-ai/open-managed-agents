package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper MCPTunnelTokenMapper -sql ./mcp_tunnel_token.xml -out ./mcp_tunnel_token.sqlmap.gen.go -dialect postgres

type mcpTunnelTokenRow struct {
	UUID          string         `db:"uuid"`
	ExternalID    string         `db:"external_id"`
	TunnelUUID    string         `db:"tunnel_uuid"`
	Version       int64          `db:"version"`
	TokenHash     []byte         `db:"token_hash"`
	Ciphertext    []byte         `db:"ciphertext"`
	Nonce         []byte         `db:"nonce"`
	WrappedDEK    []byte         `db:"wrapped_dek"`
	FormatVersion sql.NullInt32  `db:"format_version"`
	KeyProvider   sql.NullString `db:"key_provider"`
	KeyVersion    sql.NullInt64  `db:"key_version"`
	CreatedAt     time.Time      `db:"created_at"`
	RetiredAt     *time.Time     `db:"retired_at"`
	ArchivedAt    *time.Time     `db:"archived_at"`
}

type mcpTunnelTokenContextRow struct {
	UUID             string         `db:"uuid"`
	ExternalID       string         `db:"external_id"`
	TunnelUUID       string         `db:"tunnel_uuid"`
	Version          int64          `db:"version"`
	TokenHash        []byte         `db:"token_hash"`
	Ciphertext       []byte         `db:"ciphertext"`
	Nonce            []byte         `db:"nonce"`
	WrappedDEK       []byte         `db:"wrapped_dek"`
	FormatVersion    sql.NullInt32  `db:"format_version"`
	KeyProvider      sql.NullString `db:"key_provider"`
	KeyVersion       sql.NullInt64  `db:"key_version"`
	CreatedAt        time.Time      `db:"created_at"`
	RetiredAt        *time.Time     `db:"retired_at"`
	ArchivedAt       *time.Time     `db:"archived_at"`
	TunnelExternalID string         `db:"tunnel_external_id"`
	OrganizationUUID string         `db:"organization_uuid"`
	WorkspaceUUID    string         `db:"workspace_uuid"`
	TunnelArchivedAt *time.Time     `db:"tunnel_archived_at"`
}

type insertMCPTunnelTokenParams struct {
	UUID          string
	ExternalID    string
	TunnelUUID    string
	Version       int64
	TokenHash     []byte
	Ciphertext    []byte
	Nonce         []byte
	WrappedDEK    []byte
	FormatVersion int
	KeyProvider   string
	KeyVersion    int64
	CreatedAt     time.Time
}

type MCPTunnelTokenMapper interface {
	Insert(ctx context.Context, params insertMCPTunnelTokenParams) (mcpTunnelTokenRow, error)
	FindActiveByTunnelUUID(ctx context.Context, tunnelUUID string) (mcpTunnelTokenRow, error)
	FindActiveForUpdate(ctx context.Context, tunnelUUID string) (mcpTunnelTokenRow, error)
	FindByHashAndTunnelExternalID(ctx context.Context, tokenHash []byte, tunnelExternalID string) (mcpTunnelTokenContextRow, error)
	RetireActiveByTunnelUUID(ctx context.Context, tunnelUUID string, retiredAt time.Time) (int64, error)
	ArchiveByTunnelUUID(ctx context.Context, tunnelUUID string, archivedAt time.Time) error
}
