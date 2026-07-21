package db

import (
	"context"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type consoleMemberScanner interface {
	Scan(dest ...any) error
}

func scanConsoleMember(row consoleMemberScanner) (platform.OrgUser, error) {
	var user platform.OrgUser
	err := row.Scan(&user.UserUUID, &user.Email, &user.FullName, &user.Role, &user.AddedAt)
	if err != nil {
		return platform.OrgUser{}, mapNoRows(err)
	}
	return user, nil
}

func (d *DB) ListOrgUsers(ctx context.Context, orgUUID string, limit int) ([]platform.OrgUser, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.OrgUser{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := d.Pool.Query(ctx, `
		select u.uuid::text, u.email, nullif(u.name, ''), u.role, u.added_at
		from users u
		join organizations o on o.id = u.organization_id
		where (o.uuid::text = $1 or o.external_id = $1)
		  and u.deleted_at is null
		order by u.added_at asc, u.id asc
		limit $2
	`, strings.TrimSpace(orgUUID), limit)
	if err != nil {
		if isUndefinedRelationError(err) {
			return []platform.OrgUser{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	out := []platform.OrgUser{}
	for rows.Next() {
		user, err := scanConsoleMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func (d *DB) UpdateOrgUserRole(ctx context.Context, orgUUID string, userID string, role string) (*platform.OrgUser, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(userID) == "" {
		return nil, nil
	}
	user, err := scanConsoleMember(d.Pool.QueryRow(ctx, `
		with target_org as (
			select id
			from organizations
			where uuid::text = $1 or external_id = $1
			limit 1
		)
		update users u
		set role = $3,
			updated_at = now()
		from target_org o
		where u.organization_id = o.id
		  and u.deleted_at is null
		  and (
			u.uuid::text = $2
			or u.external_id = $2
			or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $2
		  )
		returning u.uuid::text, u.email, nullif(u.name, ''), u.role, u.added_at
	`, strings.TrimSpace(orgUUID), strings.TrimSpace(userID), role))
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (d *DB) RemoveOrgUser(ctx context.Context, orgUUID string, userID string) (bool, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(userID) == "" {
		return false, nil
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		with target_org as (
			select id
			from organizations
			where uuid::text = $1 or external_id = $1
			limit 1
		)
		update users u
		set deleted_at = coalesce(u.deleted_at, now()),
			updated_at = now()
		from target_org o
		where u.organization_id = o.id
		  and u.deleted_at is null
		  and (
			u.uuid::text = $2
			or u.external_id = $2
			or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $2
		  )
	`, strings.TrimSpace(orgUUID), strings.TrimSpace(userID))
	if err != nil {
		if isUndefinedRelationError(err) {
			return false, nil
		}
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		update workspace_members wm
		set deleted_at = coalesce(wm.deleted_at, now()),
			updated_at = now()
		from organizations o, users u
		where (o.uuid::text = $1 or o.external_id = $1)
		  and wm.organization_id = o.id
		  and u.organization_id = o.id
		  and wm.user_id = u.id
		  and wm.deleted_at is null
		  and (
			u.uuid::text = $2
			or u.external_id = $2
			or 'user_' || left(replace(u.uuid::text, '-', ''), 24) = $2
		  )
	`, strings.TrimSpace(orgUUID), strings.TrimSpace(userID)); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
