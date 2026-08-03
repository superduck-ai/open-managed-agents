package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminUser struct {
	UUID             uuid.UUID `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
	Email            string    `db:"email"`
	Name             string    `db:"name"`
	Role             string    `db:"role"`
	AddedAt          time.Time `db:"added_at"`
}

type ListAdminUsersParams struct {
	OrganizationUUID string
	Email            string
	AfterID          string
	BeforeID         string
	Limit            int
}

func (d *DB) GetAdminUser(ctx context.Context, organizationUUID, externalID string) (AdminUser, error) {
	return getAdminRow[AdminUser](ctx, d.sql, adminUserSelectSQL()+`
		where organization_uuid = :organization_uuid
			and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
}

func (d *DB) ListAdminUsersPage(ctx context.Context, params ListAdminUsersParams) ([]AdminUser, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"users",
		"added_at",
		"organization_uuid = :organization_uuid and external_id = :cursor_external_id and deleted_at is null",
		map[string]any{"organization_uuid": dbUUID(params.OrganizationUUID), "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminUserSelectSQL() + `
		where organization_uuid = :organization_uuid and deleted_at is null
	`
	args := map[string]any{"organization_uuid": dbUUID(params.OrganizationUUID), "limit": params.Limit + 1}
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
		where organization_uuid = :organization_uuid
			and external_id = :external_id and deleted_at is null
		returning uuid, external_id,
			organization_uuid,
			email, name, role, added_at
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID, "role": role})
}

func (d *DB) DeleteAdminUser(ctx context.Context, organizationUUID, externalID string) (AdminUser, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return AdminUser{}, err
	}
	defer tx.Rollback()
	args := map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID}
	user, err := getAdminRow[AdminUser](ctx, tx, `
		update users
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_uuid = :organization_uuid
			and external_id = :external_id and deleted_at is null
		returning uuid, external_id,
			organization_uuid,
			email, name, role, added_at
	`, args)
	if err != nil {
		return AdminUser{}, err
	}
	args["user_uuid"] = user.UUID
	if _, err := namedExecContext(ctx, tx, `
		update workspace_members
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_uuid = :organization_uuid
			and user_uuid = :user_uuid
			and deleted_at is null
	`, args); err != nil {
		return AdminUser{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminUser{}, err
	}
	return user, nil
}

func adminUserSelectSQL() string {
	return `
		select uuid, external_id,
			organization_uuid,
			email, name, role, added_at
		from users
	`
}
