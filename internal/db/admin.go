package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type AdminOrganization struct {
	ID         int64     `db:"id"`
	ExternalID string    `db:"external_id"`
	Name       string    `db:"name"`
	CreatedAt  time.Time `db:"created_at"`
}

type AdminInvite struct {
	ID             int64     `db:"id"`
	ExternalID     string    `db:"external_id"`
	OrganizationID int64     `db:"organization_id"`
	Email          string    `db:"email"`
	Role           string    `db:"role"`
	Status         string    `db:"status"`
	InvitedAt      time.Time `db:"invited_at"`
	ExpiresAt      time.Time `db:"expires_at"`
}

type AdminUser struct {
	ID             int64     `db:"id"`
	ExternalID     string    `db:"external_id"`
	OrganizationID int64     `db:"organization_id"`
	Email          string    `db:"email"`
	Name           string    `db:"name"`
	Role           string    `db:"role"`
	AddedAt        time.Time `db:"added_at"`
}

type AdminWorkspace struct {
	ID             int64           `db:"id"`
	UUID           string          `db:"uuid"`
	ExternalID     string          `db:"external_id"`
	OrganizationID int64           `db:"organization_id"`
	Name           string          `db:"name"`
	CreatedAt      time.Time       `db:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at"`
	ArchivedAt     *time.Time      `db:"archived_at"`
	CompartmentID  string          `db:"compartment_id"`
	DisplayColor   string          `db:"display_color"`
	DataResidency  json.RawMessage `db:"data_residency"`
	ExternalKeyID  *string         `db:"external_key_id"`
	Tags           json.RawMessage `db:"tags"`
}

type AdminWorkspaceMember struct {
	ID                  int64     `db:"id"`
	ExternalID          string    `db:"external_id"`
	OrganizationID      int64     `db:"organization_id"`
	WorkspaceID         int64     `db:"workspace_id"`
	WorkspaceExternalID string    `db:"workspace_external_id"`
	UserID              int64     `db:"user_id"`
	UserExternalID      string    `db:"user_external_id"`
	WorkspaceRole       string    `db:"workspace_role"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

type AdminAPIKey struct {
	ID                      int64      `db:"id"`
	ExternalID              string     `db:"external_id"`
	OrganizationID          int64      `db:"organization_id"`
	WorkspaceID             int64      `db:"workspace_id"`
	WorkspaceExternalID     string     `db:"workspace_external_id"`
	CreatedByUserExternalID *string    `db:"created_by_user_external_id"`
	Name                    string     `db:"name"`
	PartialKeyHint          string     `db:"partial_key_hint"`
	Status                  string     `db:"status"`
	CreatedAt               time.Time  `db:"created_at"`
	UpdatedAt               time.Time  `db:"updated_at"`
	ExpiresAt               *time.Time `db:"expires_at"`
}

type AdminExternalKey struct {
	ID             int64           `db:"id"`
	ExternalID     string          `db:"external_id"`
	OrganizationID int64           `db:"organization_id"`
	DisplayName    string          `db:"display_name"`
	Geo            string          `db:"geo"`
	ProviderConfig json.RawMessage `db:"provider_config"`
	CreatedAt      time.Time       `db:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at"`
}

type AdminTunnel struct {
	ID                  int64      `db:"id"`
	ExternalID          string     `db:"external_id"`
	OrganizationID      int64      `db:"organization_id"`
	WorkspaceID         *int64     `db:"workspace_id"`
	WorkspaceExternalID *string    `db:"workspace_external_id"`
	DisplayName         *string    `db:"display_name"`
	Domain              string     `db:"domain"`
	TokenID             *string    `db:"token_id"`
	TunnelToken         *string    `db:"tunnel_token"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
}

type AdminTunnelCertificate struct {
	ID               int64      `db:"id"`
	ExternalID       string     `db:"external_id"`
	OrganizationID   int64      `db:"organization_id"`
	TunnelID         int64      `db:"tunnel_id"`
	TunnelExternalID string     `db:"tunnel_external_id"`
	CACertificatePEM string     `db:"ca_certificate_pem"`
	Fingerprint      string     `db:"fingerprint"`
	ExpiresAt        *time.Time `db:"expires_at"`
	CreatedAt        time.Time  `db:"created_at"`
	ArchivedAt       *time.Time `db:"archived_at"`
}

type AdminCursor struct {
	CreatedAt time.Time `db:"created_at"`
	ID        int64     `db:"id"`
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
	return getAdminRow[AdminOrganization](ctx, d.sql, `
		select id, external_id, name, created_at
		from organizations
		where id = :organization_id
	`, map[string]any{"organization_id": organizationID})
}

func (d *DB) CreateAdminInvite(ctx context.Context, invite AdminInvite) (AdminInvite, error) {
	created, err := getAdminRow[AdminInvite](ctx, d.sql, `
		insert into organization_invites (
			external_id, organization_id, email, role, status, invited_at, expires_at
		)
		values (:external_id, :organization_id, :email, :role, :status, :invited_at, :expires_at)
		returning id, external_id, organization_id, email, role, status, invited_at, expires_at
	`, adminInviteArguments(invite))
	if isUniqueViolation(err) {
		return AdminInvite{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminInvite(ctx context.Context, organizationID int64, externalID string) (AdminInvite, error) {
	return getAdminRow[AdminInvite](ctx, d.sql, adminInviteSelectSQL()+`
		where organization_id = :organization_id and external_id = :external_id
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) ListAdminInvitesPage(ctx context.Context, params ListAdminInvitesParams) ([]AdminInvite, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"organization_invites",
		"invited_at",
		"organization_id = :organization_id and external_id = :cursor_external_id",
		map[string]any{"organization_id": params.OrganizationID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminInviteSelectSQL() + ` where organization_id = :organization_id`
	args := map[string]any{"organization_id": params.OrganizationID, "limit": params.Limit + 1}
	query = appendCursorFilter(query, args, "invited_at", params.AfterID, params.BeforeID, cursor)
	query += " order by invited_at desc, id desc limit :limit"
	invites, err := selectAdminRows[AdminInvite](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(invites, params.Limit), len(invites) > params.Limit, nil
}

func (d *DB) DeleteAdminInvite(ctx context.Context, organizationID int64, externalID string) (AdminInvite, error) {
	return getAdminRow[AdminInvite](ctx, d.sql, `
		update organization_invites
		set status = 'deleted',
			deleted_at = coalesce(deleted_at, now())
		where organization_id = :organization_id and external_id = :external_id
		returning id, external_id, organization_id, email, role, status, invited_at, expires_at
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) GetAdminUser(ctx context.Context, organizationID int64, externalID string) (AdminUser, error) {
	return getAdminRow[AdminUser](ctx, d.sql, adminUserSelectSQL()+`
		where organization_id = :organization_id and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) GetOrganizationUserRole(ctx context.Context, organizationID int64, externalID string) (string, error) {
	user, err := d.GetAdminUser(ctx, organizationID, externalID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(user.Role), nil
}

func (d *DB) ListAdminUsersPage(ctx context.Context, params ListAdminUsersParams) ([]AdminUser, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"users",
		"added_at",
		"organization_id = :organization_id and external_id = :cursor_external_id and deleted_at is null",
		map[string]any{"organization_id": params.OrganizationID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminUserSelectSQL() + ` where organization_id = :organization_id and deleted_at is null`
	args := map[string]any{"organization_id": params.OrganizationID, "limit": params.Limit + 1}
	if params.Email != "" {
		query += " and lower(email) = lower(:email)"
		args["email"] = params.Email
	}
	query = appendCursorFilter(query, args, "added_at", params.AfterID, params.BeforeID, cursor)
	query += " order by added_at desc, id desc limit :limit"
	users, err := selectAdminRows[AdminUser](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(users, params.Limit), len(users) > params.Limit, nil
}

func (d *DB) UpdateAdminUserRole(ctx context.Context, organizationID int64, externalID, role string) (AdminUser, error) {
	return getAdminRow[AdminUser](ctx, d.sql, `
		update users
		set role = :role,
			updated_at = now()
		where organization_id = :organization_id and external_id = :external_id and deleted_at is null
		returning id, external_id, organization_id, email, name, role, added_at
	`, map[string]any{"organization_id": organizationID, "external_id": externalID, "role": role})
}

func (d *DB) DeleteAdminUser(ctx context.Context, organizationID int64, externalID string) (AdminUser, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return AdminUser{}, err
	}
	defer tx.Rollback()
	args := map[string]any{"organization_id": organizationID, "external_id": externalID}
	user, err := getAdminRow[AdminUser](ctx, tx, `
		update users
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = :organization_id and external_id = :external_id and deleted_at is null
		returning id, external_id, organization_id, email, name, role, added_at
	`, args)
	if err != nil {
		return AdminUser{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		update workspace_members
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = :organization_id and user_external_id = :external_id and deleted_at is null
	`, args); err != nil {
		return AdminUser{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminUser{}, err
	}
	return user, nil
}

func (d *DB) CreateAdminWorkspace(ctx context.Context, workspace AdminWorkspace) (AdminWorkspace, error) {
	created, err := getAdminRow[AdminWorkspace](ctx, d.sql, `
		insert into workspaces (
			uuid, external_id, organization_id, name, created_at, updated_at,
			compartment_id, display_color, data_residency, external_key_id, tags
		)
		values (
			:uuid, :external_id, :organization_id, :name, :created_at, :created_at,
			:compartment_id, :display_color, CAST(:data_residency AS jsonb), :external_key_id, CAST(:tags AS jsonb)
		)
		returning id, CAST(uuid AS text) as uuid, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, data_residency, external_key_id, tags
	`, adminWorkspaceArguments(workspace))
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspace(ctx context.Context, organizationID int64, externalID string) (AdminWorkspace, error) {
	return getAdminRow[AdminWorkspace](ctx, d.sql, adminWorkspaceSelectSQL()+`
		where organization_id = :organization_id
			and (external_id = :external_id or CAST(uuid AS text) = :external_id)
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) ListAdminWorkspacesPage(ctx context.Context, params ListAdminWorkspacesParams) ([]AdminWorkspace, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"workspaces",
		"created_at",
		"organization_id = :organization_id and external_id = :cursor_external_id",
		map[string]any{"organization_id": params.OrganizationID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceSelectSQL() + ` where organization_id = :organization_id`
	args := map[string]any{"organization_id": params.OrganizationID, "limit": params.Limit + 1}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	query = appendCursorFilter(query, args, "created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by created_at desc, id desc limit :limit"
	workspaces, err := selectAdminRows[AdminWorkspace](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(workspaces, params.Limit), len(workspaces) > params.Limit, nil
}

func (d *DB) UpdateAdminWorkspace(ctx context.Context, organizationID int64, externalID string, next AdminWorkspace) (AdminWorkspace, error) {
	args := adminWorkspaceArguments(next)
	args["organization_id"] = organizationID
	args["external_id"] = externalID
	updated, err := getAdminRow[AdminWorkspace](ctx, d.sql, `
		update workspaces
		set name = :name,
			data_residency = CAST(:data_residency AS jsonb),
			external_key_id = :external_key_id,
			tags = CAST(:tags AS jsonb),
			updated_at = :updated_at
		where organization_id = :organization_id and external_id = :external_id
		returning id, CAST(uuid AS text) as uuid, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, data_residency, external_key_id, tags
	`, args)
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return updated, err
}

func (d *DB) ArchiveAdminWorkspace(ctx context.Context, organizationID int64, externalID string) (AdminWorkspace, error) {
	return getAdminRow[AdminWorkspace](ctx, d.sql, `
		update workspaces
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where organization_id = :organization_id and external_id = :external_id
		returning id, CAST(uuid AS text) as uuid, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, data_residency, external_key_id, tags
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) CreateAdminWorkspaceMember(ctx context.Context, member AdminWorkspaceMember) (AdminWorkspaceMember, error) {
	created, err := getAdminRow[AdminWorkspaceMember](ctx, d.sql, `
		insert into workspace_members (
			external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
		)
		values (
			:external_id, :organization_id, :workspace_id, :workspace_external_id,
			:user_id, :user_external_id, :workspace_role, :created_at, :created_at
		)
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
	`, adminWorkspaceMemberArguments(member))
	if isUniqueViolation(err) {
		return AdminWorkspaceMember{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspaceMember(ctx context.Context, organizationID int64, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	return getAdminRow[AdminWorkspaceMember](ctx, d.sql, adminWorkspaceMemberSelectSQL()+`
		where organization_id = :organization_id
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
	`, map[string]any{
		"organization_id":       organizationID,
		"workspace_external_id": workspaceExternalID,
		"user_external_id":      userExternalID,
	})
}

func (d *DB) ListAdminWorkspaceMembersPage(ctx context.Context, params ListAdminMembersParams) ([]AdminWorkspaceMember, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"workspace_members",
		"created_at",
		"workspace_id = :workspace_id and user_external_id = :cursor_external_id and deleted_at is null",
		map[string]any{"workspace_id": params.WorkspaceID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceMemberSelectSQL() + `
		where organization_id = :organization_id and workspace_id = :workspace_id and deleted_at is null
	`
	args := map[string]any{
		"organization_id": params.OrganizationID,
		"workspace_id":    params.WorkspaceID,
		"limit":           params.Limit + 1,
	}
	query = appendCursorFilter(query, args, "created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by created_at desc, id desc limit :limit"
	members, err := selectAdminRows[AdminWorkspaceMember](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(members, params.Limit), len(members) > params.Limit, nil
}

func (d *DB) UpdateAdminWorkspaceMember(ctx context.Context, organizationID int64, workspaceExternalID, userExternalID, role string) (AdminWorkspaceMember, error) {
	return getAdminRow[AdminWorkspaceMember](ctx, d.sql, `
		update workspace_members
		set workspace_role = :workspace_role,
			updated_at = now()
		where organization_id = :organization_id
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
	`, map[string]any{
		"organization_id":       organizationID,
		"workspace_external_id": workspaceExternalID,
		"user_external_id":      userExternalID,
		"workspace_role":        role,
	})
}

func (d *DB) DeleteAdminWorkspaceMember(ctx context.Context, organizationID int64, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	return getAdminRow[AdminWorkspaceMember](ctx, d.sql, `
		update workspace_members
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = :organization_id
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			user_id, user_external_id, workspace_role, created_at, updated_at
	`, map[string]any{
		"organization_id":       organizationID,
		"workspace_external_id": workspaceExternalID,
		"user_external_id":      userExternalID,
	})
}

func (d *DB) GetAdminAPIKey(ctx context.Context, organizationID int64, externalID string) (AdminAPIKey, error) {
	return getAdminRow[AdminAPIKey](ctx, d.sql, adminAPIKeySelectSQL()+`
		where w.organization_id = :organization_id and ak.external_id = :external_id
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) ListAdminAPIKeysPage(ctx context.Context, params ListAdminAPIKeysParams) ([]AdminAPIKey, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"api_keys",
		"created_at",
		"external_id = :cursor_external_id",
		map[string]any{"cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminAPIKeySelectSQL() + ` where w.organization_id = :organization_id`
	args := map[string]any{"organization_id": params.OrganizationID, "limit": params.Limit + 1}
	if params.WorkspaceID != "" {
		query += " and w.external_id = :workspace_external_id"
		args["workspace_external_id"] = params.WorkspaceID
	}
	if params.CreatedByUserID != "" {
		query += " and u.external_id = :created_by_user_external_id"
		args["created_by_user_external_id"] = params.CreatedByUserID
	}
	if params.Status != "" {
		query += " and ak.status = :status"
		args["status"] = params.Status
	}
	query = appendCursorFilter(query, args, "ak.created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by ak.created_at desc, ak.id desc limit :limit"
	keys, err := selectAdminRows[AdminAPIKey](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, nil
}

func (d *DB) UpdateAdminAPIKey(ctx context.Context, organizationID int64, externalID string, setName bool, name string, setStatus bool, status string) (AdminAPIKey, error) {
	return getAdminRow[AdminAPIKey](ctx, d.sql, `
		with updated as (
			update api_keys ak
			set name = case when :set_name then :name else ak.name end,
				status = case when :set_status then :status else ak.status end,
				updated_at = now()
			from workspaces w
			where ak.workspace_id = w.id
				and w.organization_id = :organization_id
				and ak.external_id = :external_id
			returning ak.id, ak.external_id, ak.workspace_id, ak.created_by_user_id,
				ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		)
		select ak.id, ak.external_id, w.organization_id, ak.workspace_id,
			w.external_id as workspace_external_id,
			u.external_id as created_by_user_external_id,
			ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		from updated ak
		join workspaces w on w.id = ak.workspace_id
		left join users u on u.id = ak.created_by_user_id
	`, map[string]any{
		"organization_id": organizationID,
		"external_id":     externalID,
		"set_name":        setName,
		"name":            name,
		"set_status":      setStatus,
		"status":          status,
	})
}

func (d *DB) CreateAdminExternalKey(ctx context.Context, key AdminExternalKey) (AdminExternalKey, error) {
	created, err := getAdminRow[AdminExternalKey](ctx, d.sql, `
		insert into external_keys (
			external_id, organization_id, display_name, geo, provider_config, created_at, updated_at
		)
		values (
			:external_id, :organization_id, :display_name, :geo,
			CAST(:provider_config AS jsonb), :created_at, :created_at
		)
		returning id, external_id, organization_id, display_name, geo, provider_config, created_at, updated_at
	`, adminExternalKeyArguments(key))
	if isUniqueViolation(err) {
		return AdminExternalKey{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminExternalKey(ctx context.Context, organizationID int64, externalID string) (AdminExternalKey, error) {
	return getAdminRow[AdminExternalKey](ctx, d.sql, adminExternalKeySelectSQL()+`
		where organization_id = :organization_id and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) ListAdminExternalKeysPage(ctx context.Context, params ListAdminOffsetParams) ([]AdminExternalKey, bool, error) {
	keys, err := selectAdminRows[AdminExternalKey](ctx, d.sql, adminExternalKeySelectSQL()+`
		where organization_id = :organization_id and deleted_at is null
		order by created_at desc, id desc
		limit :limit offset :offset
	`, map[string]any{
		"organization_id": params.OrganizationID,
		"limit":           params.Limit + 1,
		"offset":          params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, nil
}

func (d *DB) UpdateAdminExternalKey(ctx context.Context, organizationID int64, externalID string, next AdminExternalKey) (AdminExternalKey, error) {
	args := adminExternalKeyArguments(next)
	args["organization_id"] = organizationID
	args["external_id"] = externalID
	return getAdminRow[AdminExternalKey](ctx, d.sql, `
		update external_keys
		set display_name = :display_name,
			geo = :geo,
			provider_config = CAST(:provider_config AS jsonb),
			updated_at = :updated_at
		where organization_id = :organization_id and external_id = :external_id and deleted_at is null
		returning id, external_id, organization_id, display_name, geo, provider_config, created_at, updated_at
	`, args)
}

func (d *DB) DeleteAdminExternalKey(ctx context.Context, organizationID int64, externalID string) error {
	affected, err := namedExecRowsAffected(ctx, d.sql, `
		update external_keys
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_id = :organization_id and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CountAdminExternalKeyWorkspaceRefs(ctx context.Context, organizationID int64, externalID string) (int, error) {
	var count int
	err := namedGetContext(ctx, d.sql, &count, `
		select count(*)
		from workspaces
		where organization_id = :organization_id and external_key_id = :external_id
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
	return count, err
}

func (d *DB) GetAdminTunnel(ctx context.Context, organizationID int64, externalID string) (AdminTunnel, error) {
	return getAdminRow[AdminTunnel](ctx, d.sql, adminTunnelSelectSQL()+`
		where organization_id = :organization_id and external_id = :external_id
	`, map[string]any{"organization_id": organizationID, "external_id": externalID})
}

func (d *DB) ListAdminTunnelsPage(ctx context.Context, params ListAdminTunnelsParams) ([]AdminTunnel, bool, error) {
	query := adminTunnelSelectSQL() + ` where organization_id = :organization_id`
	args := map[string]any{
		"organization_id": params.OrganizationID,
		"limit":           params.Limit + 1,
		"offset":          params.Offset,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.WorkspaceID != "" {
		query += " and workspace_external_id = :workspace_external_id"
		args["workspace_external_id"] = params.WorkspaceID
	}
	query += " order by created_at desc, id desc limit :limit offset :offset"
	tunnels, err := selectAdminRows[AdminTunnel](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(tunnels, params.Limit), len(tunnels) > params.Limit, nil
}

func (d *DB) SetAdminTunnelToken(ctx context.Context, organizationID int64, externalID, tokenID, token string) (AdminTunnel, error) {
	return getAdminRow[AdminTunnel](ctx, d.sql, `
		update mcp_tunnels
		set token_id = :token_id,
			tunnel_token = :tunnel_token,
			updated_at = now()
		where organization_id = :organization_id and external_id = :external_id and archived_at is null
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, map[string]any{
		"organization_id": organizationID,
		"external_id":     externalID,
		"token_id":        tokenID,
		"tunnel_token":    token,
	})
}

func (d *DB) ArchiveAdminTunnel(ctx context.Context, organizationID int64, externalID string) (AdminTunnel, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return AdminTunnel{}, err
	}
	defer tx.Rollback()
	args := map[string]any{"organization_id": organizationID, "external_id": externalID}
	tunnel, err := getAdminRow[AdminTunnel](ctx, tx, `
		update mcp_tunnels
		set archived_at = coalesce(archived_at, now()),
			token_id = null,
			tunnel_token = null,
			updated_at = now()
		where organization_id = :organization_id and external_id = :external_id
		returning id, external_id, organization_id, workspace_id, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, args)
	if err != nil {
		return AdminTunnel{}, err
	}
	args["tunnel_id"] = tunnel.ID
	if _, err := namedExecContext(ctx, tx, `
		update mcp_tunnel_certificates
		set archived_at = coalesce(archived_at, now())
		where organization_id = :organization_id and tunnel_id = :tunnel_id and archived_at is null
	`, args); err != nil {
		return AdminTunnel{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminTunnel{}, err
	}
	return tunnel, nil
}

func (d *DB) CreateAdminTunnelCertificate(ctx context.Context, cert AdminTunnelCertificate) (AdminTunnelCertificate, error) {
	created, err := getAdminRow[AdminTunnelCertificate](ctx, d.sql, `
		insert into mcp_tunnel_certificates (
			external_id, organization_id, tunnel_id, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at
		)
		values (
			:external_id, :organization_id, :tunnel_id, :tunnel_external_id,
			:ca_certificate_pem, :fingerprint, :expires_at, :created_at
		)
		returning id, external_id, organization_id, tunnel_id, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, adminTunnelCertificateArguments(cert))
	if isUniqueViolation(err) {
		return AdminTunnelCertificate{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminTunnelCertificate(ctx context.Context, organizationID int64, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	return getAdminRow[AdminTunnelCertificate](ctx, d.sql, adminTunnelCertificateSelectSQL()+`
		where organization_id = :organization_id
			and tunnel_external_id = :tunnel_external_id
			and external_id = :external_id
	`, map[string]any{
		"organization_id":    organizationID,
		"tunnel_external_id": tunnelExternalID,
		"external_id":        certExternalID,
	})
}

func (d *DB) ListAdminTunnelCertificatesPage(ctx context.Context, params ListAdminTunnelCertificatesParams) ([]AdminTunnelCertificate, bool, error) {
	query := adminTunnelCertificateSelectSQL() + `
		where organization_id = :organization_id and tunnel_id = :tunnel_id
	`
	args := map[string]any{
		"organization_id": params.OrganizationID,
		"tunnel_id":       params.TunnelID,
		"limit":           params.Limit + 1,
		"offset":          params.Offset,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	query += " order by created_at desc, id desc limit :limit offset :offset"
	certs, err := selectAdminRows[AdminTunnelCertificate](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(certs, params.Limit), len(certs) > params.Limit, nil
}

func (d *DB) ArchiveAdminTunnelCertificate(ctx context.Context, organizationID int64, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	return getAdminRow[AdminTunnelCertificate](ctx, d.sql, `
		update mcp_tunnel_certificates
		set archived_at = coalesce(archived_at, now())
		where organization_id = :organization_id
			and tunnel_external_id = :tunnel_external_id
			and external_id = :external_id
		returning id, external_id, organization_id, tunnel_id, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, map[string]any{
		"organization_id":    organizationID,
		"tunnel_external_id": tunnelExternalID,
		"external_id":        certExternalID,
	})
}

func (d *DB) CountActiveAdminTunnelCertificates(ctx context.Context, organizationID, tunnelID int64) (int, error) {
	var count int
	err := namedGetContext(ctx, d.sql, &count, `
		select count(*)
		from mcp_tunnel_certificates
		where organization_id = :organization_id and tunnel_id = :tunnel_id and archived_at is null
	`, map[string]any{"organization_id": organizationID, "tunnel_id": tunnelID})
	return count, err
}

func (d *DB) adminCursor(
	ctx context.Context,
	table, timeColumn, where string,
	arguments map[string]any,
	externalID string,
) (*AdminCursor, bool, error) {
	if externalID == "" {
		return nil, false, nil
	}
	query := fmt.Sprintf("select id, %s as created_at from %s where %s", timeColumn, table, where)
	var cursor AdminCursor
	if err := namedGetContext(ctx, d.sql, &cursor, query, arguments); errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	return &cursor, true, nil
}

func appendCursorFilter(
	query string,
	arguments map[string]any,
	column, afterID, beforeID string,
	cursor *AdminCursor,
) string {
	if afterID == "" && beforeID == "" {
		return query
	}
	if cursor == nil {
		return query
	}
	idColumn := "id"
	if dot := strings.LastIndex(column, "."); dot > 0 {
		idColumn = column[:dot] + ".id"
	}
	if afterID != "" {
		query += fmt.Sprintf(
			" and (%s < :cursor_created_at or (%s = :cursor_created_at and %s < :cursor_id))",
			column,
			column,
			idColumn,
		)
	} else {
		query += fmt.Sprintf(
			" and (%s > :cursor_created_at or (%s = :cursor_created_at and %s > :cursor_id))",
			column,
			column,
			idColumn,
		)
	}
	arguments["cursor_created_at"] = cursor.CreatedAt
	arguments["cursor_id"] = cursor.ID
	return query
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
		select id, CAST(uuid AS text) as uuid, external_id, organization_id, name, created_at, updated_at,
			archived_at, compartment_id, display_color, data_residency, external_key_id, tags
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
		select ak.id, ak.external_id, w.organization_id, ak.workspace_id,
			w.external_id as workspace_external_id,
			u.external_id as created_by_user_external_id,
			ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
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

func getAdminRow[T any](
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (T, error) {
	var row T
	err := namedGetContext(ctx, database, &row, query, arguments)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrNotFound
	}
	return row, err
}

func selectAdminRows[T any](
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]T, error) {
	var rows []T
	err := namedSelectContext(ctx, database, &rows, query, arguments)
	return rows, err
}

func adminInviteArguments(invite AdminInvite) map[string]any {
	return map[string]any{
		"external_id":     invite.ExternalID,
		"organization_id": invite.OrganizationID,
		"email":           invite.Email,
		"role":            invite.Role,
		"status":          invite.Status,
		"invited_at":      invite.InvitedAt,
		"expires_at":      invite.ExpiresAt,
	}
}

func adminWorkspaceArguments(workspace AdminWorkspace) map[string]any {
	return map[string]any{
		"uuid":            workspace.UUID,
		"external_id":     workspace.ExternalID,
		"organization_id": workspace.OrganizationID,
		"name":            workspace.Name,
		"created_at":      workspace.CreatedAt,
		"updated_at":      workspace.UpdatedAt,
		"compartment_id":  workspace.CompartmentID,
		"display_color":   workspace.DisplayColor,
		"data_residency":  jsonArg(workspace.DataResidency),
		"external_key_id": workspace.ExternalKeyID,
		"tags":            jsonArg(workspace.Tags),
	}
}

func adminWorkspaceMemberArguments(member AdminWorkspaceMember) map[string]any {
	return map[string]any{
		"external_id":           member.ExternalID,
		"organization_id":       member.OrganizationID,
		"workspace_id":          member.WorkspaceID,
		"workspace_external_id": member.WorkspaceExternalID,
		"user_id":               member.UserID,
		"user_external_id":      member.UserExternalID,
		"workspace_role":        member.WorkspaceRole,
		"created_at":            member.CreatedAt,
	}
}

func adminExternalKeyArguments(key AdminExternalKey) map[string]any {
	return map[string]any{
		"external_id":     key.ExternalID,
		"organization_id": key.OrganizationID,
		"display_name":    key.DisplayName,
		"geo":             key.Geo,
		"provider_config": jsonArg(key.ProviderConfig),
		"created_at":      key.CreatedAt,
		"updated_at":      key.UpdatedAt,
	}
}

func adminTunnelCertificateArguments(cert AdminTunnelCertificate) map[string]any {
	return map[string]any{
		"external_id":        cert.ExternalID,
		"organization_id":    cert.OrganizationID,
		"tunnel_id":          cert.TunnelID,
		"tunnel_external_id": cert.TunnelExternalID,
		"ca_certificate_pem": cert.CACertificatePEM,
		"fingerprint":        cert.Fingerprint,
		"expires_at":         cert.ExpiresAt,
		"created_at":         cert.CreatedAt,
	}
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
