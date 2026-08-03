package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminWorkspaceMember struct {
	UUID                uuid.UUID `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	OrganizationUUID    uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID       uuid.UUID `db:"workspace_uuid"`
	WorkspaceExternalID string    `db:"workspace_external_id"`
	UserUUID            uuid.UUID `db:"user_uuid"`
	UserExternalID      string    `db:"user_external_id"`
	WorkspaceRole       string    `db:"workspace_role"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

type ListAdminMembersParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	AfterID          string
	BeforeID         string
	Limit            int
}

func (d *DB) CreateAdminWorkspaceMember(ctx context.Context, member AdminWorkspaceMember) (AdminWorkspaceMember, error) {
	created, err := getAdminRow[AdminWorkspaceMember](ctx, d.sql, `
		insert into workspace_members (
			external_id, organization_uuid, workspace_uuid, workspace_external_id,
			user_uuid, user_external_id, workspace_role, created_at, updated_at
		)
		values (
			:external_id, :organization_uuid, :workspace_uuid,
			:workspace_external_id, :user_uuid, :user_external_id,
			:workspace_role, :created_at, :created_at
		)
		returning uuid, external_id,
			organization_uuid,
			workspace_uuid, workspace_external_id,
			user_uuid, user_external_id,
			workspace_role, created_at, updated_at
	`, adminWorkspaceMemberArguments(member))
	if isUniqueViolation(err) {
		return AdminWorkspaceMember{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	return getAdminRow[AdminWorkspaceMember](ctx, d.sql, adminWorkspaceMemberSelectSQL()+`
		where organization_uuid = :organization_uuid
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
	`, map[string]any{
		"organization_uuid":     dbUUID(organizationUUID),
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
		"workspace_uuid = :workspace_uuid and user_external_id = :cursor_external_id and deleted_at is null",
		map[string]any{"workspace_uuid": dbUUID(params.WorkspaceUUID), "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminWorkspaceMemberSelectSQL() + `
		where organization_uuid = :organization_uuid
			and workspace_uuid = :workspace_uuid
			and deleted_at is null
	`
	args := map[string]any{
		"organization_uuid": dbUUID(params.OrganizationUUID),
		"workspace_uuid":    dbUUID(params.WorkspaceUUID),
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
		where organization_uuid = :organization_uuid
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
		returning uuid, external_id,
			organization_uuid,
			workspace_uuid, workspace_external_id,
			user_uuid, user_external_id,
			workspace_role, created_at, updated_at
	`, map[string]any{
		"organization_uuid":     dbUUID(organizationUUID),
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
		where organization_uuid = :organization_uuid
			and workspace_external_id = :workspace_external_id
			and user_external_id = :user_external_id
			and deleted_at is null
		returning uuid, external_id,
			organization_uuid,
			workspace_uuid, workspace_external_id,
			user_uuid, user_external_id,
			workspace_role, created_at, updated_at
	`, map[string]any{
		"organization_uuid":     dbUUID(organizationUUID),
		"workspace_external_id": workspaceExternalID,
		"user_external_id":      userExternalID,
	})
}

func adminWorkspaceMemberSelectSQL() string {
	return `
		select uuid, external_id,
			organization_uuid,
			workspace_uuid, workspace_external_id,
			user_uuid, user_external_id,
			workspace_role, created_at, updated_at
		from workspace_members
	`
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
