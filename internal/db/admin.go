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
	UUID      string    `db:"uuid"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

type AdminInvite struct {
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	Email            string    `db:"email"`
	Role             string    `db:"role"`
	Status           string    `db:"status"`
	InvitedAt        time.Time `db:"invited_at"`
	ExpiresAt        time.Time `db:"expires_at"`
}

type AdminUser struct {
	UUID             string    `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID string    `db:"organization_uuid"`
	Email            string    `db:"email"`
	Name             string    `db:"name"`
	Role             string    `db:"role"`
	AddedAt          time.Time `db:"added_at"`
}

type AdminWorkspace struct {
	UUID             string          `db:"uuid"`
	ExternalID       string          `db:"external_id"`
	OrganizationUUID string          `db:"organization_uuid"`
	Name             string          `db:"name"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
	ArchivedAt       *time.Time      `db:"archived_at"`
	CompartmentID    string          `db:"compartment_id"`
	DisplayColor     string          `db:"display_color"`
	DataResidency    json.RawMessage `db:"data_residency"`
	ExternalKeyID    *string         `db:"external_key_id"`
	Tags             json.RawMessage `db:"tags"`
}

type AdminWorkspaceMember struct {
	UUID                string    `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	OrganizationUUID    string    `db:"organization_uuid"`
	WorkspaceUUID       string    `db:"workspace_uuid"`
	WorkspaceExternalID string    `db:"workspace_external_id"`
	UserUUID            string    `db:"user_uuid"`
	UserExternalID      string    `db:"user_external_id"`
	WorkspaceRole       string    `db:"workspace_role"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

type AdminAPIKey struct {
	UUID                    string     `db:"uuid"`
	ExternalID              string     `db:"external_id"`
	WorkspaceUUID           string     `db:"workspace_uuid"`
	WorkspaceExternalID     string     `db:"workspace_external_id"`
	CreatedByUserUUID       *string    `db:"created_by_user_uuid"`
	CreatedByUserExternalID *string    `db:"created_by_user_external_id"`
	Name                    string     `db:"name"`
	PartialKeyHint          string     `db:"partial_key_hint"`
	Status                  string     `db:"status"`
	CreatedAt               time.Time  `db:"created_at"`
	UpdatedAt               time.Time  `db:"updated_at"`
	ExpiresAt               *time.Time `db:"expires_at"`
}

type AdminExternalKey struct {
	UUID             string          `db:"uuid"`
	ExternalID       string          `db:"external_id"`
	OrganizationUUID string          `db:"organization_uuid"`
	DisplayName      string          `db:"display_name"`
	Geo              string          `db:"geo"`
	ProviderConfig   json.RawMessage `db:"provider_config"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
}

type AdminTunnel struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       *string    `db:"workspace_uuid"`
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
	UUID             string     `db:"uuid"`
	ExternalID       string     `db:"external_id"`
	OrganizationUUID string     `db:"organization_uuid"`
	TunnelUUID       string     `db:"tunnel_uuid"`
	TunnelExternalID string     `db:"tunnel_external_id"`
	CACertificatePEM string     `db:"ca_certificate_pem"`
	Fingerprint      string     `db:"fingerprint"`
	ExpiresAt        *time.Time `db:"expires_at"`
	CreatedAt        time.Time  `db:"created_at"`
	ArchivedAt       *time.Time `db:"archived_at"`
}

type AdminCursor struct {
	CreatedAt time.Time `db:"created_at"`
	UUID      string    `db:"uuid"`
}

type ListAdminInvitesParams struct {
	OrganizationUUID string
	AfterID          string
	BeforeID         string
	Limit            int
}

type ListAdminUsersParams struct {
	OrganizationUUID string
	Email            string
	AfterID          string
	BeforeID         string
	Limit            int
}

type ListAdminWorkspacesParams struct {
	OrganizationUUID string
	IncludeArchived  bool
	AfterID          string
	BeforeID         string
	Limit            int
}

type ListAdminMembersParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	AfterID          string
	BeforeID         string
	Limit            int
}

type ListAdminAPIKeysParams struct {
	OrganizationUUID        string
	WorkspaceExternalID     string
	CreatedByUserExternalID string
	Status                  string
	AfterID                 string
	BeforeID                string
	Limit                   int
}

type ListAdminExternalKeysParams struct {
	OrganizationUUID string
	Limit            int
	Offset           int
}

type ListAdminTunnelsParams struct {
	OrganizationUUID    string
	WorkspaceExternalID string
	IncludeArchived     bool
	Limit               int
	Offset              int
}

type ListAdminTunnelCertificatesParams struct {
	OrganizationUUID string
	TunnelUUID       string
	IncludeArchived  bool
	Limit            int
	Offset           int
}

const (
	getAdminOrganizationQuery = `
		select CAST(uuid AS text) as uuid, name, created_at
		from organizations
		where uuid = CAST(:organization_uuid AS uuid)
	`
	countAdminExternalKeyWorkspaceRefsQuery = `
		select count(*)
		from workspaces
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_key_id = :external_id
	`
)

func (d *DB) GetAdminOrganization(ctx context.Context, organizationUUID string) (AdminOrganization, error) {
	return getAdminRow[AdminOrganization](ctx, d.sql, getAdminOrganizationQuery, map[string]any{
		"organization_uuid": organizationUUID,
	})
}

func (d *DB) CreateAdminInvite(ctx context.Context, invite AdminInvite) (AdminInvite, error) {
	created, err := getAdminRow[AdminInvite](ctx, d.sql, `
		insert into organization_invites (
			external_id, organization_uuid, email, role, status, invited_at, expires_at
		)
		values (
			:external_id, CAST(:organization_uuid AS uuid), :email, :role, :status, :invited_at, :expires_at
		)
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			email, role, status, invited_at, expires_at
	`, adminInviteArguments(invite))
	if isUniqueViolation(err) {
		return AdminInvite{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminInvite(ctx context.Context, organizationUUID, externalID string) (AdminInvite, error) {
	return getAdminRow[AdminInvite](ctx, d.sql, adminInviteSelectSQL()+`
		where organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :external_id
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
}

func (d *DB) ListAdminInvitesPage(ctx context.Context, params ListAdminInvitesParams) ([]AdminInvite, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"organization_invites",
		"invited_at",
		"organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :cursor_external_id",
		map[string]any{"organization_uuid": params.OrganizationUUID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminInviteSelectSQL() + ` where organization_uuid = CAST(:organization_uuid AS uuid)`
	args := map[string]any{"organization_uuid": params.OrganizationUUID, "limit": params.Limit + 1}
	query = appendCursorFilter(query, args, "invited_at", params.AfterID, params.BeforeID, cursor)
	query += " order by invited_at desc, uuid desc limit :limit"
	invites, err := selectAdminRows[AdminInvite](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(invites, params.Limit), len(invites) > params.Limit, nil
}

func (d *DB) DeleteAdminInvite(ctx context.Context, organizationUUID, externalID string) (AdminInvite, error) {
	return getAdminRow[AdminInvite](ctx, d.sql, `
		update organization_invites
		set status = 'deleted',
			deleted_at = coalesce(deleted_at, now())
		where organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :external_id
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			email, role, status, invited_at, expires_at
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
}

func (d *DB) GetAdminUser(ctx context.Context, organizationUUID, externalID string) (AdminUser, error) {
	return getAdminRow[AdminUser](ctx, d.sql, adminUserSelectSQL()+`
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
}

func (d *DB) ListAdminUsersPage(ctx context.Context, params ListAdminUsersParams) ([]AdminUser, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"users",
		"added_at",
		"organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :cursor_external_id and deleted_at is null",
		map[string]any{"organization_uuid": params.OrganizationUUID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminUserSelectSQL() + `
		where organization_uuid = CAST(:organization_uuid AS uuid) and deleted_at is null
	`
	args := map[string]any{"organization_uuid": params.OrganizationUUID, "limit": params.Limit + 1}
	if params.Email != "" {
		query += " and lower(email) = lower(:email)"
		args["email"] = params.Email
	}
	query = appendCursorFilter(query, args, "added_at", params.AfterID, params.BeforeID, cursor)
	query += " order by added_at desc, uuid desc limit :limit"
	users, err := selectAdminRows[AdminUser](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(users, params.Limit), len(users) > params.Limit, nil
}

func (d *DB) UpdateAdminUserRole(ctx context.Context, organizationUUID, externalID, role string) (AdminUser, error) {
	return getAdminRow[AdminUser](ctx, d.sql, `
		update users
		set role = :role,
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id and deleted_at is null
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			email, name, role, added_at
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID, "role": role})
}

func (d *DB) DeleteAdminUser(ctx context.Context, organizationUUID, externalID string) (AdminUser, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return AdminUser{}, err
	}
	defer tx.Rollback()
	args := map[string]any{"organization_uuid": organizationUUID, "external_id": externalID}
	user, err := getAdminRow[AdminUser](ctx, tx, `
		update users
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id and deleted_at is null
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			email, name, role, added_at
	`, args)
	if err != nil {
		return AdminUser{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		update workspace_members
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and user_uuid = CAST(:user_uuid AS uuid)
			and deleted_at is null
	`, map[string]any{"organization_uuid": organizationUUID, "user_uuid": user.UUID}); err != nil {
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
			uuid, external_id, organization_uuid, name, created_at, updated_at,
			compartment_id, display_color, data_residency, external_key_id, tags
		)
		values (
			:uuid, :external_id, CAST(:organization_uuid AS uuid), :name, :created_at, :created_at,
			:compartment_id, :display_color, CAST(:data_residency AS jsonb), :external_key_id, CAST(:tags AS jsonb)
		)
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid, name, created_at, updated_at,
			archived_at, compartment_id, display_color, data_residency, external_key_id, tags
	`, adminWorkspaceArguments(workspace))
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspace(ctx context.Context, organizationUUID, externalID string) (AdminWorkspace, error) {
	return getAdminRow[AdminWorkspace](ctx, d.sql, adminWorkspaceSelectSQL()+`
		where w.organization_uuid = CAST(:organization_uuid AS uuid)
			and (w.external_id = :external_id or CAST(w.uuid AS text) = :external_id)
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
}

func (d *DB) ListAdminWorkspacesPage(ctx context.Context, params ListAdminWorkspacesParams) ([]AdminWorkspace, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"workspaces",
		"created_at",
		"organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :cursor_external_id",
		map[string]any{"organization_uuid": params.OrganizationUUID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceSelectSQL() + ` where w.organization_uuid = CAST(:organization_uuid AS uuid)`
	args := map[string]any{"organization_uuid": params.OrganizationUUID, "limit": params.Limit + 1}
	if !params.IncludeArchived {
		query += " and w.archived_at is null"
	}
	query = appendCursorFilter(query, args, "w.created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by w.created_at desc, w.uuid desc limit :limit"
	workspaces, err := selectAdminRows[AdminWorkspace](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(workspaces, params.Limit), len(workspaces) > params.Limit, nil
}

func (d *DB) UpdateAdminWorkspace(ctx context.Context, organizationUUID, externalID string, next AdminWorkspace) (AdminWorkspace, error) {
	args := adminWorkspaceArguments(next)
	args["organization_uuid"] = organizationUUID
	args["external_id"] = externalID
	updated, err := getAdminRow[AdminWorkspace](ctx, d.sql, `
		update workspaces
		set name = :name,
			data_residency = CAST(:data_residency AS jsonb),
			external_key_id = :external_key_id,
			tags = CAST(:tags AS jsonb),
			updated_at = :updated_at
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid, name, created_at, updated_at,
			archived_at, compartment_id, display_color, data_residency, external_key_id, tags
	`, args)
	if isUniqueViolation(err) {
		return AdminWorkspace{}, ErrDuplicate
	}
	return updated, err
}

func (d *DB) ArchiveAdminWorkspace(ctx context.Context, organizationUUID, externalID string) (AdminWorkspace, error) {
	return getAdminRow[AdminWorkspace](ctx, d.sql, `
		update workspaces
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid, name, created_at, updated_at,
			archived_at, compartment_id, display_color, data_residency, external_key_id, tags
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
}

func (d *DB) CreateAdminWorkspaceMember(ctx context.Context, member AdminWorkspaceMember) (AdminWorkspaceMember, error) {
	created, err := getAdminRow[AdminWorkspaceMember](ctx, d.sql, `
		insert into workspace_members (
			external_id, organization_uuid, workspace_uuid, workspace_external_id,
			user_uuid, user_external_id, workspace_role, created_at, updated_at
		)
		values (
			:external_id, CAST(:organization_uuid AS uuid), CAST(:workspace_uuid AS uuid),
			:workspace_external_id, CAST(:user_uuid AS uuid), :user_external_id,
			:workspace_role, :created_at, :created_at
		)
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(workspace_uuid AS text) as workspace_uuid, workspace_external_id,
			CAST(user_uuid AS text) as user_uuid, user_external_id,
			workspace_role, created_at, updated_at
	`, adminWorkspaceMemberArguments(member))
	if isUniqueViolation(err) {
		return AdminWorkspaceMember{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	return getAdminRow[AdminWorkspaceMember](ctx, d.sql, adminWorkspaceMemberSelectSQL()+`
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
	`, map[string]any{
		"organization_uuid":     organizationUUID,
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
		"workspace_uuid = CAST(:workspace_uuid AS uuid) and user_external_id = :cursor_external_id and deleted_at is null",
		map[string]any{"workspace_uuid": params.WorkspaceUUID, "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceMemberSelectSQL() + `
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and workspace_uuid = CAST(:workspace_uuid AS uuid)
			and deleted_at is null
	`
	args := map[string]any{
		"organization_uuid": params.OrganizationUUID,
		"workspace_uuid":    params.WorkspaceUUID,
		"limit":             params.Limit + 1,
	}
	query = appendCursorFilter(query, args, "created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by created_at desc, uuid desc limit :limit"
	members, err := selectAdminRows[AdminWorkspaceMember](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(members, params.Limit), len(members) > params.Limit, nil
}

func (d *DB) UpdateAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceExternalID, userExternalID, role string) (AdminWorkspaceMember, error) {
	return getAdminRow[AdminWorkspaceMember](ctx, d.sql, `
		update workspace_members
		set workspace_role = :workspace_role,
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(workspace_uuid AS text) as workspace_uuid, workspace_external_id,
			CAST(user_uuid AS text) as user_uuid, user_external_id,
			workspace_role, created_at, updated_at
	`, map[string]any{
		"organization_uuid":     organizationUUID,
		"workspace_external_id": workspaceExternalID,
		"user_external_id":      userExternalID,
		"workspace_role":        role,
	})
}

func (d *DB) DeleteAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	return getAdminRow[AdminWorkspaceMember](ctx, d.sql, `
		update workspace_members
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(workspace_uuid AS text) as workspace_uuid, workspace_external_id,
			CAST(user_uuid AS text) as user_uuid, user_external_id,
			workspace_role, created_at, updated_at
	`, map[string]any{
		"organization_uuid":     organizationUUID,
		"workspace_external_id": workspaceExternalID,
		"user_external_id":      userExternalID,
	})
}

func (d *DB) GetAdminAPIKey(ctx context.Context, organizationUUID, externalID string) (AdminAPIKey, error) {
	return getAdminRow[AdminAPIKey](ctx, d.sql, adminAPIKeySelectSQL()+`
		where w.organization_uuid = CAST(:organization_uuid AS uuid)
			and ak.external_id = :external_id
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
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
	query := adminAPIKeySelectSQL() + ` where w.organization_uuid = CAST(:organization_uuid AS uuid)`
	args := map[string]any{"organization_uuid": params.OrganizationUUID, "limit": params.Limit + 1}
	if params.WorkspaceExternalID != "" {
		query += " and w.external_id = :workspace_external_id"
		args["workspace_external_id"] = params.WorkspaceExternalID
	}
	if params.CreatedByUserExternalID != "" {
		query += " and u.external_id = :created_by_user_external_id"
		args["created_by_user_external_id"] = params.CreatedByUserExternalID
	}
	if params.Status != "" {
		query += " and ak.status = :status"
		args["status"] = params.Status
	}
	query = appendCursorFilter(query, args, "ak.created_at", params.AfterID, params.BeforeID, cursor)
	query += " order by ak.created_at desc, ak.uuid desc limit :limit"
	keys, err := selectAdminRows[AdminAPIKey](ctx, d.sql, query, args)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, nil
}

func (d *DB) UpdateAdminAPIKey(ctx context.Context, organizationUUID, externalID string, setName bool, name string, setStatus bool, status string) (AdminAPIKey, error) {
	return getAdminRow[AdminAPIKey](ctx, d.sql, `
		with updated as (
			update api_keys ak
			set name = case when :set_name then :name else ak.name end,
				status = case when :set_status then :status else ak.status end,
				updated_at = now()
			from workspaces w
			where ak.workspace_uuid = w.uuid
				and w.organization_uuid = CAST(:organization_uuid AS uuid)
				and ak.external_id = :external_id
			returning CAST(ak.uuid AS text) as uuid, ak.external_id,
				CAST(ak.workspace_uuid AS text) as workspace_uuid,
				CAST(ak.created_by_user_uuid AS text) as created_by_user_uuid,
				ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		)
		select ak.uuid, ak.external_id, ak.workspace_uuid,
			w.external_id as workspace_external_id,
			ak.created_by_user_uuid,
			u.external_id as created_by_user_external_id,
			ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		from updated ak
		join workspaces w on CAST(w.uuid AS text) = ak.workspace_uuid
		left join users u on CAST(u.uuid AS text) = ak.created_by_user_uuid
	`, map[string]any{
		"organization_uuid": organizationUUID,
		"external_id":       externalID,
		"set_name":          setName,
		"name":              name,
		"set_status":        setStatus,
		"status":            status,
	})
}

func (d *DB) CreateAdminExternalKey(ctx context.Context, key AdminExternalKey) (AdminExternalKey, error) {
	created, err := getAdminRow[AdminExternalKey](ctx, d.sql, `
		insert into external_keys (
			external_id, organization_uuid, display_name, geo, provider_config, created_at, updated_at
		)
		values (
			:external_id, CAST(:organization_uuid AS uuid), :display_name, :geo,
			CAST(:provider_config AS jsonb), :created_at, :created_at
		)
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			display_name, geo, provider_config, created_at, updated_at
	`, adminExternalKeyArguments(key))
	if isUniqueViolation(err) {
		return AdminExternalKey{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminExternalKey(ctx context.Context, organizationUUID, externalID string) (AdminExternalKey, error) {
	return getAdminRow[AdminExternalKey](ctx, d.sql, adminExternalKeySelectSQL()+`
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
}

func (d *DB) ListAdminExternalKeysPage(ctx context.Context, params ListAdminExternalKeysParams) ([]AdminExternalKey, bool, error) {
	keys, err := selectAdminRows[AdminExternalKey](ctx, d.sql, adminExternalKeySelectSQL()+`
		where organization_uuid = CAST(:organization_uuid AS uuid) and deleted_at is null
		order by created_at desc, uuid desc
		limit :limit offset :offset
	`, map[string]any{
		"organization_uuid": params.OrganizationUUID,
		"limit":             params.Limit + 1,
		"offset":            params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, nil
}

func (d *DB) UpdateAdminExternalKey(ctx context.Context, organizationUUID, externalID string, next AdminExternalKey) (AdminExternalKey, error) {
	args := adminExternalKeyArguments(next)
	args["organization_uuid"] = organizationUUID
	args["external_id"] = externalID
	return getAdminRow[AdminExternalKey](ctx, d.sql, `
		update external_keys
		set display_name = :display_name,
			geo = :geo,
			provider_config = CAST(:provider_config AS jsonb),
			updated_at = :updated_at
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id and deleted_at is null
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			display_name, geo, provider_config, created_at, updated_at
	`, args)
}

func (d *DB) DeleteAdminExternalKey(ctx context.Context, organizationUUID, externalID string) error {
	affected, err := namedExecRowsAffected(ctx, d.sql, `
		update external_keys
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) CountAdminExternalKeyWorkspaceRefs(ctx context.Context, organizationUUID, externalID string) (int, error) {
	var count int
	err := namedGetContext(ctx, d.sql, &count, countAdminExternalKeyWorkspaceRefsQuery, map[string]any{
		"organization_uuid": organizationUUID,
		"external_id":       externalID,
	})
	return count, err
}

func (d *DB) GetAdminTunnel(ctx context.Context, organizationUUID, externalID string) (AdminTunnel, error) {
	return getAdminRow[AdminTunnel](ctx, d.sql, adminTunnelSelectSQL()+`
		where organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :external_id
	`, map[string]any{"organization_uuid": organizationUUID, "external_id": externalID})
}

func (d *DB) ListAdminTunnelsPage(ctx context.Context, params ListAdminTunnelsParams) ([]AdminTunnel, bool, error) {
	query := adminTunnelSelectSQL() + ` where organization_uuid = CAST(:organization_uuid AS uuid)`
	args := map[string]any{
		"organization_uuid": params.OrganizationUUID,
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
		where organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :external_id and archived_at is null
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(workspace_uuid AS text) as workspace_uuid, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, map[string]any{
		"organization_uuid": organizationUUID,
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
	args := map[string]any{"organization_uuid": organizationUUID, "external_id": externalID}
	tunnel, err := getAdminRow[AdminTunnel](ctx, tx, `
		update mcp_tunnels
		set archived_at = coalesce(archived_at, now()),
			token_id = null,
			tunnel_token = null,
			updated_at = now()
		where organization_uuid = CAST(:organization_uuid AS uuid) and external_id = :external_id
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(workspace_uuid AS text) as workspace_uuid, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
	`, args)
	if err != nil {
		return AdminTunnel{}, err
	}
	args["tunnel_uuid"] = tunnel.UUID
	if _, err := namedExecContext(ctx, tx, `
		update mcp_tunnel_certificates
		set archived_at = coalesce(archived_at, now())
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and tunnel_uuid = CAST(:tunnel_uuid AS uuid)
			and archived_at is null
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
			external_id, organization_uuid, tunnel_uuid, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at
		)
		values (
			:external_id, CAST(:organization_uuid AS uuid), CAST(:tunnel_uuid AS uuid), :tunnel_external_id,
			:ca_certificate_pem, :fingerprint, :expires_at, :created_at
		)
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(tunnel_uuid AS text) as tunnel_uuid, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, adminTunnelCertificateArguments(cert))
	if isUniqueViolation(err) {
		return AdminTunnelCertificate{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminTunnelCertificate(ctx context.Context, organizationUUID, tunnelExternalID, certExternalID string) (AdminTunnelCertificate, error) {
	return getAdminRow[AdminTunnelCertificate](ctx, d.sql, adminTunnelCertificateSelectSQL()+`
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and tunnel_external_id = :tunnel_external_id
			and external_id = :external_id
	`, map[string]any{
		"organization_uuid":  organizationUUID,
		"tunnel_external_id": tunnelExternalID,
		"external_id":        certExternalID,
	})
}

func (d *DB) ListAdminTunnelCertificatesPage(ctx context.Context, params ListAdminTunnelCertificatesParams) ([]AdminTunnelCertificate, bool, error) {
	query := adminTunnelCertificateSelectSQL() + `
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and tunnel_uuid = CAST(:tunnel_uuid AS uuid)
	`
	args := map[string]any{
		"organization_uuid": params.OrganizationUUID,
		"tunnel_uuid":       params.TunnelUUID,
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
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and tunnel_external_id = :tunnel_external_id
			and external_id = :external_id
		returning CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(tunnel_uuid AS text) as tunnel_uuid, tunnel_external_id,
			ca_certificate_pem, fingerprint, expires_at, created_at, archived_at
	`, map[string]any{
		"organization_uuid":  organizationUUID,
		"tunnel_external_id": tunnelExternalID,
		"external_id":        certExternalID,
	})
}

func (d *DB) CountActiveAdminTunnelCertificates(ctx context.Context, organizationUUID, tunnelUUID string) (int, error) {
	var count int
	err := namedGetContext(ctx, d.sql, &count, `
		select count(*)
		from mcp_tunnel_certificates
		where organization_uuid = CAST(:organization_uuid AS uuid)
			and tunnel_uuid = CAST(:tunnel_uuid AS uuid)
			and archived_at is null
	`, map[string]any{"organization_uuid": organizationUUID, "tunnel_uuid": tunnelUUID})
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
	query := fmt.Sprintf(
		"select CAST(uuid AS text) as uuid, %s as created_at from %s where %s",
		timeColumn,
		table,
		where,
	)
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
	uuidColumn := "uuid"
	if dot := strings.LastIndex(column, "."); dot > 0 {
		uuidColumn = column[:dot] + ".uuid"
	}
	if afterID != "" {
		query += fmt.Sprintf(
			" and (%s < :cursor_created_at or (%s = :cursor_created_at and %s < CAST(:cursor_uuid AS uuid)))",
			column,
			column,
			uuidColumn,
		)
	} else {
		query += fmt.Sprintf(
			" and (%s > :cursor_created_at or (%s = :cursor_created_at and %s > CAST(:cursor_uuid AS uuid)))",
			column,
			column,
			uuidColumn,
		)
	}
	arguments["cursor_created_at"] = cursor.CreatedAt
	arguments["cursor_uuid"] = cursor.UUID
	return query
}

func adminInviteSelectSQL() string {
	return `
		select CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			email, role, status, invited_at, expires_at
		from organization_invites
	`
}

func adminUserSelectSQL() string {
	return `
		select CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			email, name, role, added_at
		from users
	`
}

func adminWorkspaceSelectSQL() string {
	return `
		select CAST(w.uuid AS text) as uuid, w.external_id,
			CAST(w.organization_uuid AS text) as organization_uuid, w.name,
			w.created_at, w.updated_at, w.archived_at, w.compartment_id,
			w.display_color, w.data_residency, w.external_key_id, w.tags
		from workspaces w
	`
}

func adminWorkspaceMemberSelectSQL() string {
	return `
		select CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(workspace_uuid AS text) as workspace_uuid, workspace_external_id,
			CAST(user_uuid AS text) as user_uuid, user_external_id,
			workspace_role, created_at, updated_at
		from workspace_members
	`
}

func adminAPIKeySelectSQL() string {
	return `
		select CAST(ak.uuid AS text) as uuid, ak.external_id,
			CAST(ak.workspace_uuid AS text) as workspace_uuid,
			w.external_id as workspace_external_id,
			CAST(ak.created_by_user_uuid AS text) as created_by_user_uuid,
			u.external_id as created_by_user_external_id,
			ak.name, ak.partial_key_hint, ak.status, ak.created_at, ak.updated_at, ak.expires_at
		from api_keys ak
		join workspaces w on w.uuid = ak.workspace_uuid
		left join users u on u.uuid = ak.created_by_user_uuid
	`
}

func adminExternalKeySelectSQL() string {
	return `
		select CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			display_name, geo, provider_config, created_at, updated_at
		from external_keys
	`
}

func adminTunnelSelectSQL() string {
	return `
		select CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(workspace_uuid AS text) as workspace_uuid, workspace_external_id,
			display_name, domain, token_id, tunnel_token, created_at, updated_at, archived_at
		from mcp_tunnels
	`
}

func adminTunnelCertificateSelectSQL() string {
	return `
		select CAST(uuid AS text) as uuid, external_id,
			CAST(organization_uuid AS text) as organization_uuid,
			CAST(tunnel_uuid AS text) as tunnel_uuid, tunnel_external_id,
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
		"external_id":       invite.ExternalID,
		"organization_uuid": invite.OrganizationUUID,
		"email":             invite.Email,
		"role":              invite.Role,
		"status":            invite.Status,
		"invited_at":        invite.InvitedAt,
		"expires_at":        invite.ExpiresAt,
	}
}

func adminWorkspaceArguments(workspace AdminWorkspace) map[string]any {
	return map[string]any{
		"uuid":              workspace.UUID,
		"external_id":       workspace.ExternalID,
		"organization_uuid": workspace.OrganizationUUID,
		"name":              workspace.Name,
		"created_at":        workspace.CreatedAt,
		"updated_at":        workspace.UpdatedAt,
		"compartment_id":    workspace.CompartmentID,
		"display_color":     workspace.DisplayColor,
		"data_residency":    jsonArg(workspace.DataResidency),
		"external_key_id":   workspace.ExternalKeyID,
		"tags":              jsonArg(workspace.Tags),
	}
}

func adminWorkspaceMemberArguments(member AdminWorkspaceMember) map[string]any {
	return map[string]any{
		"external_id":           member.ExternalID,
		"organization_uuid":     member.OrganizationUUID,
		"workspace_uuid":        member.WorkspaceUUID,
		"workspace_external_id": member.WorkspaceExternalID,
		"user_uuid":             member.UserUUID,
		"user_external_id":      member.UserExternalID,
		"workspace_role":        member.WorkspaceRole,
		"created_at":            member.CreatedAt,
	}
}

func adminExternalKeyArguments(key AdminExternalKey) map[string]any {
	return map[string]any{
		"external_id":       key.ExternalID,
		"organization_uuid": key.OrganizationUUID,
		"display_name":      key.DisplayName,
		"geo":               key.Geo,
		"provider_config":   jsonArg(key.ProviderConfig),
		"created_at":        key.CreatedAt,
		"updated_at":        key.UpdatedAt,
	}
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
