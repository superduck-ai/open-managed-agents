package db

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/platform"

	"github.com/google/uuid"
)

const (
	listOrgUsersQuery = `
		select
			u.uuid AS user_uuid,
			u.email,
			nullif(u.name, '') AS full_name,
			u.role,
			u.added_at
		from users u
		where u.organization_uuid = :org_uuid
			and u.deleted_at is null
		order by u.added_at asc, u.uuid asc
		limit :limit
	`
	updateOrgUserRoleQuery = `
		update users u
		set role = :role,
			updated_at = now()
		where u.organization_uuid = :org_uuid
			and u.deleted_at is null
			and (
				u.uuid = :user_uuid
				or u.external_id = :user_id
				or 'user_' || left(replace(CAST(u.uuid AS text), '-', ''), 24) = :user_id
			)
		returning
			u.uuid AS user_uuid,
			u.email,
			nullif(u.name, '') AS full_name,
			u.role,
			u.added_at
	`
	removeOrgUserQuery = `
		update users u
		set deleted_at = coalesce(u.deleted_at, now()),
			updated_at = now()
		where u.organization_uuid = :org_uuid
			and u.deleted_at is null
			and (
				u.uuid = :user_uuid
				or u.external_id = :user_id
				or 'user_' || left(replace(CAST(u.uuid AS text), '-', ''), 24) = :user_id
			)
	`
	removeOrgUserWorkspaceMembershipsQuery = `
		with target_user as (
			select uuid
			from users
			where organization_uuid = :org_uuid
				and (
					uuid = :user_uuid
					or external_id = :user_id
					or 'user_' || left(replace(CAST(uuid AS text), '-', ''), 24) = :user_id
				)
			limit 1
		)
		update workspace_members wm
		set deleted_at = coalesce(wm.deleted_at, now()),
			updated_at = now()
		from target_user u
		where wm.organization_uuid = :org_uuid
			and wm.user_uuid = u.uuid
			and wm.deleted_at is null
	`
)

type consoleMemberRow struct {
	UserUUID uuid.UUID `db:"user_uuid"`
	Email    string    `db:"email"`
	FullName *string   `db:"full_name"`
	Role     string    `db:"role"`
	AddedAt  time.Time `db:"added_at"`
}

func (d *DB) ListOrgUsers(ctx context.Context, orgUUID string, limit int) ([]platform.OrgUser, error) {
	if d == nil || d.sql == nil || orgUUID == "" {
		return []platform.OrgUser{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var rows []consoleMemberRow
	err := namedSelectContext(ctx, d.sql, &rows, listOrgUsersQuery, map[string]any{
		"org_uuid": orgUUID,
		"limit":    limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]platform.OrgUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.user())
	}
	return out, nil
}

func (d *DB) UpdateOrgUserRole(ctx context.Context, orgUUID string, userID string, role string) (*platform.OrgUser, error) {
	if d == nil || d.sql == nil || orgUUID == "" || userID == "" {
		return nil, nil
	}
	user, err := getConsoleMemberSQLX(ctx, d.sql, updateOrgUserRoleQuery, map[string]any{
		"org_uuid":  orgUUID,
		"user_id":   userID,
		"user_uuid": tryParseDBUUIDIdentifier(userID),
		"role":      role,
	})
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (d *DB) RemoveOrgUser(ctx context.Context, orgUUID string, userID string) (bool, error) {
	if d == nil || d.sql == nil || orgUUID == "" || userID == "" {
		return false, nil
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	arguments := map[string]any{
		"org_uuid":  orgUUID,
		"user_id":   userID,
		"user_uuid": tryParseDBUUIDIdentifier(userID),
	}
	rowsAffected, err := namedExecRowsAffected(ctx, tx, removeOrgUserQuery, arguments)
	if err != nil {
		return false, err
	}
	if rowsAffected == 0 {
		return false, nil
	}
	if _, err := namedExecContext(ctx, tx, removeOrgUserWorkspaceMembershipsQuery, arguments); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func getConsoleMemberSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (platform.OrgUser, error) {
	var row consoleMemberRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return platform.OrgUser{}, mapNoRows(err)
	}
	return row.user(), nil
}

func (r consoleMemberRow) user() platform.OrgUser {
	return platform.OrgUser{
		UserUUID: r.UserUUID.String(),
		Email:    r.Email,
		FullName: r.FullName,
		Role:     r.Role,
		AddedAt:  r.AddedAt,
	}
}
