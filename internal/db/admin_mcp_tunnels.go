package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminTunnel struct {
	UUID                uuid.UUID     `db:"uuid"`
	ExternalID          string        `db:"external_id"`
	OrganizationUUID    uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID       uuid.NullUUID `db:"workspace_uuid"`
	WorkspaceExternalID *string       `db:"workspace_external_id"`
	DisplayName         *string       `db:"display_name"`
	Domain              string        `db:"domain"`
	TokenID             *string       `db:"token_id"`
	TunnelToken         *string       `db:"tunnel_token"`
	CreatedAt           time.Time     `db:"created_at"`
	UpdatedAt           time.Time     `db:"updated_at"`
	ArchivedAt          *time.Time    `db:"archived_at"`
}

type ListAdminTunnelsParams struct {
	OrganizationUUID    string
	WorkspaceExternalID string
	IncludeArchived     bool
	Limit               int
	Offset              int
}

func (d *DB) GetAdminTunnel(ctx context.Context, organizationUUID, externalID string) (AdminTunnel, error) {
	return getAdminRow[AdminTunnel](ctx, d.sql, adminTunnelSelectSQL()+`
		where organization_uuid = :organization_uuid and external_id = :external_id
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
}

func (d *DB) ListAdminTunnelsPage(ctx context.Context, params ListAdminTunnelsParams) ([]AdminTunnel, bool, error) {
	query := adminTunnelSelectSQL() + ` where organization_uuid = :organization_uuid`
	args := map[string]any{
		"organization_uuid": dbUUID(params.OrganizationUUID),
		"limit":             params.Limit + 1,
		"offset":            params.Offset,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.WorkspaceExternalID != "" {
		query += " and workspace_external_id = :workspace_external_id"
		args["workspace_external_id"] = params.WorkspaceExternalID
	}
	query += " order by created_at desc, uuid desc limit :limit offset :offset"
	tunnels, err := selectAdminRows[AdminTunnel](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(tunnels, params.Limit), len(tunnels) > params.Limit, nil
}

func (d *DB) SetAdminTunnelToken(ctx context.Context, organizationUUID, externalID, tokenID, token string) (AdminTunnel, error) {
	return getAdminRow[AdminTunnel](ctx, d.sql, `
		update mcp_tunnels
		set token_id = :token_id,
			tunnel_token = :tunnel_token,
			updated_at = now()
		where organization_uuid = :organization_uuid and external_id = :external_id and archived_at is null
		returning uuid, external_id,
			organization_uuid,
			workspace_uuid, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, map[string]any{
		"organization_uuid": dbUUID(organizationUUID),
		"external_id":       externalID,
		"token_id":          tokenID,
		"tunnel_token":      token,
	})
}

func (d *DB) ArchiveAdminTunnel(ctx context.Context, organizationUUID, externalID string) (AdminTunnel, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return AdminTunnel{}, err
	}
	defer tx.Rollback()
	args := map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID}
	tunnel, err := getAdminRow[AdminTunnel](ctx, tx, `
		update mcp_tunnels
		set archived_at = coalesce(archived_at, now()),
			token_id = null,
			tunnel_token = null,
			updated_at = now()
		where organization_uuid = :organization_uuid and external_id = :external_id
		returning uuid, external_id,
			organization_uuid,
			workspace_uuid, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, args)
	if err != nil {
		return AdminTunnel{}, err
	}
	args["tunnel_uuid"] = tunnel.UUID
	if _, err := namedExecContext(ctx, tx, `
		update mcp_tunnel_certificates
		set archived_at = coalesce(archived_at, now())
		where organization_uuid = :organization_uuid
			and tunnel_uuid = :tunnel_uuid
			and archived_at is null
	`, args); err != nil {
		return AdminTunnel{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminTunnel{}, err
	}
	return tunnel, nil
}

func adminTunnelSelectSQL() string {
	return `
		select uuid, external_id,
			organization_uuid,
			workspace_uuid, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
		from mcp_tunnels
	`
}
