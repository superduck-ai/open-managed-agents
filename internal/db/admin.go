package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type AdminOrganization struct {
	ID         int64
	ExternalID string
	Name       string
	CreatedAt  time.Time
}

type AdminInvite struct {
	ID             int64
	ExternalID     string
	OrganizationID int64
	Email          string
	Role           string
	Status         string
	InvitedAt      time.Time
	ExpiresAt      time.Time
}

type AdminUser struct {
	ID             int64
	ExternalID     string
	OrganizationID int64
	Email          string
	Name           string
	Role           string
	AddedAt        time.Time
}

type AdminWorkspace struct {
	ID             int64
	UUID           string
	ExternalID     string
	OrganizationID int64
	Name           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
	CompartmentID  string
	DisplayColor   string
	ExternalKeyID  *string
	Tags           json.RawMessage
}

type AdminWorkspaceMember struct {
	ID                  int64
	ExternalID          string
	OrganizationID      int64
	WorkspaceID         int64
	WorkspaceExternalID string
	UserID              int64
	UserExternalID      string
	WorkspaceRole       string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type AdminAPIKey struct {
	ID                      int64
	ExternalID              string
	OrganizationID          int64
	WorkspaceID             int64
	WorkspaceExternalID     string
	CreatedByUserExternalID *string
	Name                    string
	PartialKeyHint          string
	Status                  string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	ExpiresAt               *time.Time
}

type AdminExternalKey struct {
	ID             int64
	ExternalID     string
	OrganizationID int64
	DisplayName    string
	Geo            string
	ProviderConfig json.RawMessage
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AdminTunnel struct {
	ID                  int64
	ExternalID          string
	OrganizationID      int64
	WorkspaceID         *int64
	WorkspaceExternalID *string
	DisplayName         *string
	Domain              string
	TokenID             *string
	TunnelToken         *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
}

type AdminTunnelCertificate struct {
	ID               int64
	ExternalID       string
	OrganizationID   int64
	TunnelID         int64
	TunnelExternalID string
	CACertificatePEM string
	Fingerprint      string
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	ArchivedAt       *time.Time
}

type AdminCursor struct {
	CreatedAt time.Time
	ID        int64
}

type ListAdminInvitesParams struct {
	OrganizationID int64
	AfterID        string
	BeforeID       string
	Limit          int
}

type ListAdminUsersParams struct {
	OrganizationID int64
	Email          string
	AfterID        string
	BeforeID       string
	Limit          int
}

type ListAdminWorkspacesParams struct {
	OrganizationID  int64
	IncludeArchived bool
	AfterID         string
	BeforeID        string
	Limit           int
}

type ListAdminMembersParams struct {
	OrganizationID int64
	WorkspaceID    int64
	AfterID        string
	BeforeID       string
	Limit          int
}

type ListAdminAPIKeysParams struct {
	OrganizationID  int64
	WorkspaceID     string
	CreatedByUserID string
	Status          string
	AfterID         string
	BeforeID        string
	Limit           int
}

type ListAdminOffsetParams struct {
	OrganizationID int64
	Limit          int
	Offset         int
}

type ListAdminTunnelsParams struct {
	OrganizationID  int64
	WorkspaceID     string
	IncludeArchived bool
	Limit           int
	Offset          int
}

type ListAdminTunnelCertificatesParams struct {
	OrganizationID  int64
	TunnelID        int64
	IncludeArchived bool
	Limit           int
	Offset          int
}

func (d *DB) GetAdminOrganization(ctx context.Context, organizationID int64) (AdminOrganization, error) {
	var org AdminOrganization
	err := d.Pool.QueryRow(ctx, `
		select id, external_id, name, created_at
		from organizations
		where id = $1
	`, organizationID).Scan(&org.ID, &org.ExternalID, &org.Name, &org.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminOrganization{}, ErrNotFound
	}
	return org, err
}

func (d *DB) CreateAdminInvite(ctx context.Context, invite AdminInvite) (AdminInvite, error) {
	created, err := scanAdminInvite(d.Pool.QueryRow(ctx, `
		insert into organization_invites (
			external_id, organization_id, email, role, status, invited_at, expires_at
		)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning id, external_id, organization_id, email, role, status, invited_at, expires_at
	`, invite.ExternalID, invite.OrganizationID, invite.Email, invite.Role, invite.Status, invite.InvitedAt, invite.ExpiresAt))
	if isUniqueViolation(err) {
		return AdminInvite{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminInvite(ctx context.Context, organizationID int64, externalID string) (AdminInvite, error) {
	return scanAdminInvite(d.Pool.QueryRow(ctx, adminInviteSelectSQL()+`
		where organization_id = $1 and external_id = $2
	`, organizationID, externalID))
}

func (d *DB) ListAdminInvitesPage(ctx context.Context, params ListAdminInvitesParams) ([]AdminInvite, bool, error) {
	cursor, cursorOK, err := d.adminCursor(ctx, "organization_invites", "invited_at", "organization_id = $1 and external_id = $2", params.OrganizationID, firstNonEmpty(params.AfterID, params.BeforeID))
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminInviteSelectSQL() + ` where organization_id = $1`
	args := []any{params.OrganizationID}
	query, args = appendCursorFilter(query, args, "invited_at", params.AfterID, params.BeforeID, cursor)
	query += fmt.Sprintf(" order by invited_at desc, id desc limit $%d", len(args)+1)
	args = append(args, params.Limit+1)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	invites, err := scanAdminInviteRows(rows)
	return trimAdminPage(invites, params.Limit), len(invites) > params.Limit, err
}

func (d *DB) DeleteAdminInvite(ctx context.Context, organizationID int64, externalID string) (AdminInvite, error) {
	return scanAdminInvite(d.Pool.QueryRow(ctx, `
		update organization_invites
		set status = 'deleted',
			deleted_at = coalesce(deleted_at, now())
		where organization_id = $1 and external_id = $2
		returning id, external_id, organization_id, email, role, status, invited_at, expires_at
	`, organizationID, externalID))
}

func (d *DB) GetAdminUser(ctx context.Context, organizationID int64, externalID string) (AdminUser, error) {
	return scanAdminUser(d.Pool.QueryRow(ctx, adminUserSelectSQL()+`
		where organization_id = $1 and external_id = $2 and deleted_at is null
	`, organizationID, externalID))
}

func (d *DB) ListAdminUsersPage(ctx context.Context, params ListAdminUsersParams) ([]AdminUser, bool, error) {
	cursor, cursorOK, err := d.adminCursor(ctx, "users", "added_at", "organization_id = $1 and external_id = $2 and deleted_at is null", params.OrganizationID, firstNonEmpty(params.AfterID, params.BeforeID))
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminUserSelectSQL() + ` where organization_id = $1 and deleted_at is null`
	args := []any{params.OrganizationID}
	if params.Email != "" {
		query += fmt.Sprintf(" and lower(email) = lower($%d)", len(args)+1)
		args = append(args, params.Email)
	}
	query, args = appendCursorFilter(query, args, "added_at", params.AfterID, params.BeforeID, cursor)
	query += fmt.Sprintf(" order by added_at desc, id desc limit $%d", len(args)+1)
	args = append(args, params.Limit+1)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	users, err := scanAdminUserRows(rows)
	return trimAdminPage(users, params.Limit), len(users) > params.Limit, err
}

func (d *DB) UpdateAdminUserRole(ctx context.Context, organizationID int64, externalID, role string) (AdminUser, error) {
	return scanAdminUser(d.Pool.QueryRow(ctx, `
		update users
		set role = $3,
			updated_at = now()
		where organization_id = $1 and external_id = $2 and deleted_at is null
		returning id, external_id, organization_id, email, name, role, added_at
	`, organizationID, externalID, role))
}

func (d *DB) DeleteAdminUser(ctx context.Context, organizationID int64, externalID string) (AdminUser, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return AdminUser{}, err
	}
	defer tx.Rollback(ctx)
	user, err := scanAdminUser(tx.QueryRow(ctx, `
		update users
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = $1 and external_id = $2 and deleted_at is null
		returning id, external_id, organization_id, email, name, role, added_at
	`, organizationID, externalID))
	if err != nil {
		return AdminUser{}, err
	}
	if _, err := tx.Exec(ctx, `
		update workspace_members
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = $1 and user_external_id = $2 and deleted_at is null
	`, organizationID, externalID); err != nil {
		return AdminUser{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminUser{}, err
	}
	return user, nil
}

func (d *DB) CreateAdminWorkspace(ctx context.Context, workspace AdminWorkspace) (AdminWorkspace, error) {
	created, err := scanAdminWorkspace(d.Pool.QueryRow(ctx, `
		insert into workspaces (
			uuid, external_id, organization_id, name, created_at, updated_at,
			compartment_id, display_color, external_key_id, tags
		)
		values ($1, $2, $3, $4, $5, $5, $6, $7, $8, $9::jsonb)
		returning id, uuid::text, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, external_key_id, tags
	`, workspace.UUID, workspace.ExternalID, workspace.OrganizationID, workspace.Name, workspace.CreatedAt,
		workspace.CompartmentID, workspace.DisplayColor, workspace.ExternalKeyID, jsonArg(workspace.Tags)))
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspace(ctx context.Context, organizationID int64, externalID string) (AdminWorkspace, error) {
	return scanAdminWorkspace(d.Pool.QueryRow(ctx, adminWorkspaceSelectSQL()+`
		where organization_id = $1 and (external_id = $2 or uuid::text = $2)
	`, organizationID, externalID))
}

func (d *DB) ListAdminWorkspacesPage(ctx context.Context, params ListAdminWorkspacesParams) ([]AdminWorkspace, bool, error) {
	cursor, cursorOK, err := d.adminCursor(ctx, "workspaces", "created_at", "organization_id = $1 and external_id = $2", params.OrganizationID, firstNonEmpty(params.AfterID, params.BeforeID))
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceSelectSQL() + ` where organization_id = $1`
	args := []any{params.OrganizationID}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	query, args = appendCursorFilter(query, args, "created_at", params.AfterID, params.BeforeID, cursor)
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", len(args)+1)
	args = append(args, params.Limit+1)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	workspaces, err := scanAdminWorkspaceRows(rows)
	return trimAdminPage(workspaces, params.Limit), len(workspaces) > params.Limit, err
}

func (d *DB) UpdateAdminWorkspace(ctx context.Context, organizationID int64, externalID string, next AdminWorkspace) (AdminWorkspace, error) {
	updated, err := scanAdminWorkspace(d.Pool.QueryRow(ctx, `
		update workspaces
		set name = $3,
			external_key_id = $4,
			tags = $5::jsonb,
			updated_at = $6
		where organization_id = $1 and external_id = $2
		returning id, uuid::text, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, external_key_id, tags
	`, organizationID, externalID, next.Name, next.ExternalKeyID, jsonArg(next.Tags), next.UpdatedAt))
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return updated, err
}

func (d *DB) ArchiveAdminWorkspace(ctx context.Context, organizationID int64, externalID string) (AdminWorkspace, error) {
	return scanAdminWorkspace(d.Pool.QueryRow(ctx, `
		update workspaces
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where organization_id = $1 and external_id = $2
		returning id, uuid::text, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, external_key_id, tags
	`, organizationID, externalID))
}

func (d *DB) CreateAdminWorkspaceMember(ctx context.Context, member AdminWorkspaceMember) (AdminWorkspaceMember, error) {
	created, err := scanAdminWorkspaceMember(d.Pool.QueryRow(ctx, `
		insert into workspace_members (
			external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
	`, member.ExternalID, member.OrganizationID, member.WorkspaceID, member.WorkspaceExternalID,
		member.UserID, member.UserExternalID, member.WorkspaceRole, member.CreatedAt))
	if isUniqueViolation(err) {
		return AdminWorkspaceMember{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspaceMember(ctx context.Context, organizationID int64, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	return scanAdminWorkspaceMember(d.Pool.QueryRow(ctx, adminWorkspaceMemberSelectSQL()+`
		where organization_id = $1 and workspace_external_id = $2 and user_external_id = $3 and deleted_at is null
	`, organizationID, workspaceExternalID, userExternalID))
}

func (d *DB) ListAdminWorkspaceMembersPage(ctx context.Context, params ListAdminMembersParams) ([]AdminWorkspaceMember, bool, error) {
	cursor, cursorOK, err := d.adminCursor(ctx, "workspace_members", "created_at", "workspace_id = $1 and user_external_id = $2 and deleted_at is null", params.WorkspaceID, firstNonEmpty(params.AfterID, params.BeforeID))
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceMemberSelectSQL() + ` where organization_id = $1 and workspace_id = $2 and deleted_at is null`
	args := []any{params.OrganizationID, params.WorkspaceID}
	query, args = appendCursorFilter(query, args, "created_at", params.AfterID, params.BeforeID, cursor)
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", len(args)+1)
	args = append(args, params.Limit+1)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	members, err := scanAdminWorkspaceMemberRows(rows)
	return trimAdminPage(members, params.Limit), len(members) > params.Limit, err
}

func (d *DB) UpdateAdminWorkspaceMember(ctx context.Context, organizationID int64, workspaceExternalID, userExternalID, role string) (AdminWorkspaceMember, error) {
	return scanAdminWorkspaceMember(d.Pool.QueryRow(ctx, `
		update workspace_members
		set workspace_role = $4,
			updated_at = now()
		where organization_id = $1 and workspace_external_id = $2 and user_external_id = $3 and deleted_at is null
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
	`, organizationID, workspaceExternalID, userExternalID, role))
}

func (d *DB) DeleteAdminWorkspaceMember(ctx context.Context, organizationID int64, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	return scanAdminWorkspaceMember(d.Pool.QueryRow(ctx, `
		update workspace_members
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = $1 and workspace_external_id = $2 and user_external_id = $3 and deleted_at is null
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
	`, organizationID, workspaceExternalID, userExternalID))
}

func (d *DB) GetAdminAPIKey(ctx context.Context, organizationID int64, externalID string) (AdminAPIKey, error) {
	return scanAdminAPIKey(d.Pool.QueryRow(ctx, adminAPIKeySelectSQL()+`
		where w.organization_id = $1 and ak.external_id = $2
	`, organizationID, externalID))
}

func (d *DB) ListAdminAPIKeysPage(ctx context.Context, params ListAdminAPIKeysParams) ([]AdminAPIKey, bool, error) {
	cursor, cursorOK, err := d.adminCursor(ctx, "api_keys", "created_at", "external_id = $1", firstNonEmpty(params.AfterID, params.BeforeID))
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminAPIKeySelectSQL() + ` where w.organization_id = $1`
	args := []any{params.OrganizationID}
	if params.WorkspaceID != "" {
		query += fmt.Sprintf(" and w.external_id = $%d", len(args)+1)
		args = append(args, params.WorkspaceID)
	}
	if params.CreatedByUserID != "" {
		query += fmt.Sprintf(" and u.external_id = $%d", len(args)+1)
		args = append(args, params.CreatedByUserID)
	}
	if params.Status != "" {
		query += fmt.Sprintf(" and ak.status = $%d", len(args)+1)
		args = append(args, params.Status)
	}
	query, args = appendCursorFilter(query, args, "ak.created_at", params.AfterID, params.BeforeID, cursor)
	query += fmt.Sprintf(" order by ak.created_at desc, ak.id desc limit $%d", len(args)+1)
	args = append(args, params.Limit+1)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	keys, err := scanAdminAPIKeyRows(rows)
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, err
}

func (d *DB) UpdateAdminAPIKey(ctx context.Context, organizationID int64, externalID string, setName bool, name string, setStatus bool, status string) (AdminAPIKey, error) {
	return scanAdminAPIKey(d.Pool.QueryRow(ctx, `
		with updated as (
			update api_keys ak
			set name = case when $3 then $4 else ak.name end,
				status = case when $5 then $6 else ak.status end,
				updated_at = now()
			from workspaces w
			where ak.workspace_id = w.id
				and w.organization_id = $1
				and ak.external_id = $2
			returning ak.id, ak.external_id, ak.workspace_id, ak.created_by_user_id,
				ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		)
		select ak.id, ak.external_id, w.organization_id, ak.workspace_id, w.external_id,
			u.external_id, ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		from updated ak
		join workspaces w on w.id = ak.workspace_id
		left join users u on u.id = ak.created_by_user_id
	`, organizationID, externalID, setName, name, setStatus, status))
}

func (d *DB) CreateAdminExternalKey(ctx context.Context, key AdminExternalKey) (AdminExternalKey, error) {
	created, err := scanAdminExternalKey(d.Pool.QueryRow(ctx, `
		insert into external_keys (
			external_id, organization_id, display_name, geo, provider_config, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5::jsonb, $6, $6)
		returning id, external_id, organization_id, display_name, geo, provider_config, created_at, updated_at
	`, key.ExternalID, key.OrganizationID, key.DisplayName, key.Geo, jsonArg(key.ProviderConfig), key.CreatedAt))
	if isUniqueViolation(err) {
		return AdminExternalKey{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminExternalKey(ctx context.Context, organizationID int64, externalID string) (AdminExternalKey, error) {
	return scanAdminExternalKey(d.Pool.QueryRow(ctx, adminExternalKeySelectSQL()+`
		where organization_id = $1 and external_id = $2 and deleted_at is null
	`, organizationID, externalID))
}

func (d *DB) ListAdminExternalKeysPage(ctx context.Context, params ListAdminOffsetParams) ([]AdminExternalKey, bool, error) {
	rows, err := d.Pool.Query(ctx, adminExternalKeySelectSQL()+`
		where organization_id = $1 and deleted_at is null
		order by created_at desc, id desc
		limit $2 offset $3
	`, params.OrganizationID, params.Limit+1, params.Offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	keys, err := scanAdminExternalKeyRows(rows)
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, err
}

func (d *DB) UpdateAdminExternalKey(ctx context.Context, organizationID int64, externalID string, next AdminExternalKey) (AdminExternalKey, error) {
	return scanAdminExternalKey(d.Pool.QueryRow(ctx, `
		update external_keys
		set display_name = $3,
			geo = $4,
			provider_config = $5::jsonb,
			updated_at = $6
		where organization_id = $1 and external_id = $2 and deleted_at is null
		returning id, external_id, organization_id, display_name, geo, provider_config, created_at, updated_at
	`, organizationID, externalID, next.DisplayName, next.Geo, jsonArg(next.ProviderConfig), next.UpdatedAt))
}

func (d *DB) DeleteAdminExternalKey(ctx context.Context, organizationID int64, externalID string) error {
	tag, err := d.Pool.Exec(ctx, `
		update external_keys
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = $1 and external_id = $2 and deleted_at is null
	`, organizationID, externalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CountAdminExternalKeyWorkspaceRefs(ctx context.Context, organizationID int64, externalID string) (int, error) {
	var count int
	err := d.Pool.QueryRow(ctx, `
		select count(*)
		from workspaces
		where organization_id = $1 and external_key_id = $2
	`, organizationID, externalID).Scan(&count)
	return count, err
}

func (d *DB) GetAdminTunnel(ctx context.Context, organizationID int64, externalID string) (AdminTunnel, error) {
	return scanAdminTunnel(d.Pool.QueryRow(ctx, adminTunnelSelectSQL()+`
		where organization_id = $1 and external_id = $2
	`, organizationID, externalID))
}

func (d *DB) ListAdminTunnelsPage(ctx context.Context, params ListAdminTunnelsParams) ([]AdminTunnel, bool, error) {
	query := adminTunnelSelectSQL() + ` where organization_id = $1`
	args := []any{params.OrganizationID}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.WorkspaceID != "" {
		query += fmt.Sprintf(" and workspace_external_id = $%d", len(args)+1)
		args = append(args, params.WorkspaceID)
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d offset $%d", len(args)+1, len(args)+2)
	args = append(args, params.Limit+1, params.Offset)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	tunnels, err := scanAdminTunnelRows(rows)
	return trimAdminPage(tunnels, params.Limit), len(tunnels) > params.Limit, err
}

func (d *DB) SetAdminTunnelToken(ctx context.Context, organizationID int64, externalID, tokenID, token string) (AdminTunnel, error) {
	return scanAdminTunnel(d.Pool.QueryRow(ctx, `
		update mcp_tunnels
		set token_id = $3,
			tunnel_token = $4,
			updated_at = now()
		where organization_id = $1 and external_id = $2 and archived_at is null
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, organizationID, externalID, tokenID, token))
}

func (d *DB) ArchiveAdminTunnel(ctx context.Context, organizationID int64, externalID string) (AdminTunnel, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return AdminTunnel{}, err
	}
	defer tx.Rollback(ctx)
	tunnel, err := scanAdminTunnel(tx.QueryRow(ctx, `
		update mcp_tunnels
		set archived_at = coalesce(archived_at, now()),
			token_id = null,
			tunnel_token = null,
			updated_at = now()
		where organization_id = $1 and external_id = $2
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, organizationID, externalID))
	if err != nil {
		return AdminTunnel{}, err
	}
	if _, err := tx.Exec(ctx, `
		update mcp_tunnel_certificates
		set archived_at = coalesce(archived_at, now())
		where organization_id = $1 and tunnel_id = $2 and archived_at is null
	`, organizationID, tunnel.ID); err != nil {
		return AdminTunnel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminTunnel{}, err
	}
	return tunnel, nil
}

func (d *DB) CreateAdminTunnelCertificate(ctx context.Context, cert AdminTunnelCertificate) (AdminTunnelCertificate, error) {
	created, err := scanAdminTunnelCertificate(d.Pool.QueryRow(ctx, `
		insert into mcp_tunnel_certificates (
			external_id, organization_id, tunnel_id, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		returning id, external_id, organization_id, tunnel_id, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, cert.ExternalID, cert.OrganizationID, cert.TunnelID, cert.TunnelExternalID,
		cert.CACertificatePEM, cert.Fingerprint, cert.ExpiresAt, cert.CreatedAt))
	if isUniqueViolation(err) {
		return AdminTunnelCertificate{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminTunnelCertificate(ctx context.Context, organizationID int64, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	return scanAdminTunnelCertificate(d.Pool.QueryRow(ctx, adminTunnelCertificateSelectSQL()+`
		where organization_id = $1 and tunnel_external_id = $2 and external_id = $3
	`, organizationID, tunnelExternalID, certExternalID))
}

func (d *DB) ListAdminTunnelCertificatesPage(ctx context.Context, params ListAdminTunnelCertificatesParams) ([]AdminTunnelCertificate, bool, error) {
	query := adminTunnelCertificateSelectSQL() + ` where organization_id = $1 and tunnel_id = $2`
	args := []any{params.OrganizationID, params.TunnelID}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d offset $%d", len(args)+1, len(args)+2)
	args = append(args, params.Limit+1, params.Offset)
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	certs, err := scanAdminTunnelCertificateRows(rows)
	return trimAdminPage(certs, params.Limit), len(certs) > params.Limit, err
}

func (d *DB) ArchiveAdminTunnelCertificate(ctx context.Context, organizationID int64, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	return scanAdminTunnelCertificate(d.Pool.QueryRow(ctx, `
		update mcp_tunnel_certificates
		set archived_at = coalesce(archived_at, now())
		where organization_id = $1 and tunnel_external_id = $2 and external_id = $3
		returning id, external_id, organization_id, tunnel_id, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, organizationID, tunnelExternalID, certExternalID))
}

func (d *DB) CountActiveAdminTunnelCertificates(ctx context.Context, organizationID, tunnelID int64) (int, error) {
	var count int
	err := d.Pool.QueryRow(ctx, `
		select count(*)
		from mcp_tunnel_certificates
		where organization_id = $1 and tunnel_id = $2 and archived_at is null
	`, organizationID, tunnelID).Scan(&count)
	return count, err
}

func (d *DB) adminCursor(ctx context.Context, table, timeColumn, where string, args ...any) (*AdminCursor, bool, error) {
	if len(args) == 0 {
		return nil, false, nil
	}
	last := args[len(args)-1]
	if value, ok := last.(string); ok && value == "" {
		return nil, false, nil
	}
	query := fmt.Sprintf("select id, %s from %s where %s", timeColumn, table, where)
	var cursor AdminCursor
	if err := d.Pool.QueryRow(ctx, query, args...).Scan(&cursor.ID, &cursor.CreatedAt); errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	return &cursor, true, nil
}

func appendCursorFilter(query string, args []any, column, afterID, beforeID string, cursor *AdminCursor) (string, []any) {
	if afterID == "" && beforeID == "" {
		return query, args
	}
	if cursor == nil {
		return query, args
	}
	next := len(args) + 1
	idColumn := "id"
	if dot := strings.LastIndex(column, "."); dot > 0 {
		idColumn = column[:dot] + ".id"
	}
	if afterID != "" {
		query += fmt.Sprintf(" and (%s < $%d or (%s = $%d and %s < $%d))", column, next, column, next, idColumn, next+1)
	} else {
		query += fmt.Sprintf(" and (%s > $%d or (%s = $%d and %s > $%d))", column, next, column, next, idColumn, next+1)
	}
	args = append(args, cursor.CreatedAt, cursor.ID)
	return query, args
}

func adminInviteSelectSQL() string {
	return `
		select id, external_id, organization_id, email, role, status, invited_at, expires_at
		from organization_invites
	`
}

func adminUserSelectSQL() string {
	return `
		select id, external_id, organization_id, email, name, role, added_at
		from users
	`
}

func adminWorkspaceSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, external_key_id, tags
		from workspaces
	`
}

func adminWorkspaceMemberSelectSQL() string {
	return `
		select id, external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
		from workspace_members
	`
}

func adminAPIKeySelectSQL() string {
	return `
		select ak.id, ak.external_id, w.organization_id, ak.workspace_id, w.external_id,
			u.external_id, ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		from api_keys ak
		join workspaces w on w.id = ak.workspace_id
		left join users u on u.id = ak.created_by_user_id
	`
}

func adminExternalKeySelectSQL() string {
	return `
		select id, external_id, organization_id, display_name, geo, provider_config, created_at, updated_at
		from external_keys
	`
}

func adminTunnelSelectSQL() string {
	return `
		select id, external_id, organization_id, workspace_id, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
		from mcp_tunnels
	`
}

func adminTunnelCertificateSelectSQL() string {
	return `
		select id, external_id, organization_id, tunnel_id, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
		from mcp_tunnel_certificates
	`
}

type adminScanner interface {
	Scan(dest ...any) error
}

type adminRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanAdminInvite(row adminScanner) (AdminInvite, error) {
	var invite AdminInvite
	err := row.Scan(&invite.ID, &invite.ExternalID, &invite.OrganizationID, &invite.Email, &invite.Role, &invite.Status, &invite.InvitedAt, &invite.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminInvite{}, ErrNotFound
	}
	return invite, err
}

func scanAdminInviteRows(rows adminRows) ([]AdminInvite, error) {
	var invites []AdminInvite
	for rows.Next() {
		invite, err := scanAdminInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}
	return invites, rows.Err()
}

func scanAdminUser(row adminScanner) (AdminUser, error) {
	var user AdminUser
	err := row.Scan(&user.ID, &user.ExternalID, &user.OrganizationID, &user.Email, &user.Name, &user.Role, &user.AddedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminUser{}, ErrNotFound
	}
	return user, err
}

func scanAdminUserRows(rows adminRows) ([]AdminUser, error) {
	var users []AdminUser
	for rows.Next() {
		user, err := scanAdminUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func scanAdminWorkspace(row adminScanner) (AdminWorkspace, error) {
	var workspace AdminWorkspace
	var tags []byte
	err := row.Scan(&workspace.ID, &workspace.UUID, &workspace.ExternalID, &workspace.OrganizationID, &workspace.Name,
		&workspace.CreatedAt, &workspace.UpdatedAt, &workspace.ArchivedAt, &workspace.CompartmentID, &workspace.DisplayColor,
		&workspace.ExternalKeyID, &tags)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminWorkspace{}, ErrNotFound
	}
	workspace.Tags = copyRaw(tags)
	return workspace, err
}

func scanAdminWorkspaceRows(rows adminRows) ([]AdminWorkspace, error) {
	var workspaces []AdminWorkspace
	for rows.Next() {
		workspace, err := scanAdminWorkspace(rows)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	return workspaces, rows.Err()
}

func scanAdminWorkspaceMember(row adminScanner) (AdminWorkspaceMember, error) {
	var member AdminWorkspaceMember
	err := row.Scan(&member.ID, &member.ExternalID, &member.OrganizationID, &member.WorkspaceID, &member.WorkspaceExternalID,
		&member.UserID, &member.UserExternalID, &member.WorkspaceRole, &member.CreatedAt, &member.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminWorkspaceMember{}, ErrNotFound
	}
	return member, err
}

func scanAdminWorkspaceMemberRows(rows adminRows) ([]AdminWorkspaceMember, error) {
	var members []AdminWorkspaceMember
	for rows.Next() {
		member, err := scanAdminWorkspaceMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func scanAdminAPIKey(row adminScanner) (AdminAPIKey, error) {
	var key AdminAPIKey
	err := row.Scan(&key.ID, &key.ExternalID, &key.OrganizationID, &key.WorkspaceID, &key.WorkspaceExternalID,
		&key.CreatedByUserExternalID, &key.Name, &key.PartialKeyHint, &key.Status, &key.CreatedAt, &key.UpdatedAt, &key.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminAPIKey{}, ErrNotFound
	}
	return key, err
}

func scanAdminAPIKeyRows(rows adminRows) ([]AdminAPIKey, error) {
	var keys []AdminAPIKey
	for rows.Next() {
		key, err := scanAdminAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func scanAdminExternalKey(row adminScanner) (AdminExternalKey, error) {
	var key AdminExternalKey
	var providerConfig []byte
	err := row.Scan(&key.ID, &key.ExternalID, &key.OrganizationID, &key.DisplayName, &key.Geo, &providerConfig, &key.CreatedAt, &key.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminExternalKey{}, ErrNotFound
	}
	key.ProviderConfig = copyRaw(providerConfig)
	return key, err
}

func scanAdminExternalKeyRows(rows adminRows) ([]AdminExternalKey, error) {
	var keys []AdminExternalKey
	for rows.Next() {
		key, err := scanAdminExternalKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func scanAdminTunnel(row adminScanner) (AdminTunnel, error) {
	var tunnel AdminTunnel
	err := row.Scan(&tunnel.ID, &tunnel.ExternalID, &tunnel.OrganizationID, &tunnel.WorkspaceID, &tunnel.WorkspaceExternalID,
		&tunnel.DisplayName, &tunnel.Domain, &tunnel.TokenID, &tunnel.TunnelToken, &tunnel.CreatedAt, &tunnel.UpdatedAt, &tunnel.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminTunnel{}, ErrNotFound
	}
	return tunnel, err
}

func scanAdminTunnelRows(rows adminRows) ([]AdminTunnel, error) {
	var tunnels []AdminTunnel
	for rows.Next() {
		tunnel, err := scanAdminTunnel(rows)
		if err != nil {
			return nil, err
		}
		tunnels = append(tunnels, tunnel)
	}
	return tunnels, rows.Err()
}

func scanAdminTunnelCertificate(row adminScanner) (AdminTunnelCertificate, error) {
	var cert AdminTunnelCertificate
	err := row.Scan(&cert.ID, &cert.ExternalID, &cert.OrganizationID, &cert.TunnelID, &cert.TunnelExternalID,
		&cert.CACertificatePEM, &cert.Fingerprint, &cert.ExpiresAt, &cert.CreatedAt, &cert.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminTunnelCertificate{}, ErrNotFound
	}
	return cert, err
}

func scanAdminTunnelCertificateRows(rows adminRows) ([]AdminTunnelCertificate, error) {
	var certs []AdminTunnelCertificate
	for rows.Next() {
		cert, err := scanAdminTunnelCertificate(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return certs, rows.Err()
}

func trimAdminPage[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
