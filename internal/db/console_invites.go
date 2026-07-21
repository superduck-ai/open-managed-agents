package db

import (
	"context"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type consoleInviteScanner interface {
	Scan(dest ...any) error
}

func scanConsoleInvite(row consoleInviteScanner) (platform.ConsoleInvite, error) {
	var invite platform.ConsoleInvite
	err := row.Scan(&invite.ID, &invite.Email, &invite.Role, &invite.Status, &invite.InvitedAt, &invite.ExpiresAt)
	if err != nil {
		return platform.ConsoleInvite{}, mapNoRows(err)
	}
	return invite, nil
}

func (d *DB) ListConsoleInvites(ctx context.Context, orgUUID string, status string, limit int) ([]platform.ConsoleInvite, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.ConsoleInvite{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	query := `
		select i.external_id, i.email, i.role, i.status, i.invited_at, i.expires_at
		from organization_invites i
		join organizations o on o.id = i.organization_id
		where (o.uuid::text = $1 or o.external_id = $1)
	`
	args := []any{strings.TrimSpace(orgUUID)}
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "":
		query += ` and i.deleted_at is null`
	case "pending":
		query += ` and i.deleted_at is null and i.status = 'pending' and i.expires_at > now()`
	case "expired":
		query += ` and i.deleted_at is null and (i.status = 'expired' or (i.status = 'pending' and i.expires_at <= now()))`
	case "accepted":
		query += ` and i.deleted_at is null and i.status = 'accepted'`
	case "deleted":
		query += ` and (i.status = 'deleted' or i.deleted_at is not null)`
	default:
		return []platform.ConsoleInvite{}, nil
	}
	query += ` order by i.invited_at desc, i.id desc limit $2`
	args = append(args, limit)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		if isUndefinedRelationError(err) {
			return []platform.ConsoleInvite{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	out := []platform.ConsoleInvite{}
	for rows.Next() {
		invite, err := scanConsoleInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, invite)
	}
	return out, rows.Err()
}

func (d *DB) CreateConsoleInvite(ctx context.Context, input platform.CreateConsoleInviteInput) (platform.ConsoleInvite, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(input.OrgUUID) == "" ||
		strings.TrimSpace(input.Email) == "" || strings.TrimSpace(input.Role) == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	externalID, err := ids.New("invite_")
	if err != nil {
		return platform.ConsoleInvite{}, err
	}
	now := time.Now().UTC()
	invite, err := scanConsoleInvite(d.Pool.QueryRow(ctx, `
		with org as (
			select id
			from organizations
			where uuid::text = $1 or external_id = $1
			limit 1
		)
		insert into organization_invites (
			external_id, organization_id, email, role, status, invited_at, expires_at
		)
		select $2, org.id, $3, $4, 'pending', $5, $6
		from org
		returning external_id, email, role, status, invited_at, expires_at
	`, strings.TrimSpace(input.OrgUUID), externalID, strings.TrimSpace(input.Email), strings.TrimSpace(input.Role), now, now.Add(21*24*time.Hour)))
	if isUniqueViolation(err) {
		return platform.ConsoleInvite{}, err
	}
	return invite, err
}

func (d *DB) ResendConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(inviteID) == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	now := time.Now().UTC()
	return scanConsoleInvite(d.Pool.QueryRow(ctx, `
		update organization_invites i
		set status = 'pending',
			invited_at = $3,
			expires_at = $4
		from organizations o
		where i.organization_id = o.id
			and (o.uuid::text = $1 or o.external_id = $1)
			and i.external_id = $2
			and i.deleted_at is null
		returning i.external_id, i.email, i.role, i.status, i.invited_at, i.expires_at
	`, strings.TrimSpace(orgUUID), strings.TrimSpace(inviteID), now, now.Add(21*24*time.Hour)))
}

func (d *DB) DeleteConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(inviteID) == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	return scanConsoleInvite(d.Pool.QueryRow(ctx, `
		update organization_invites i
		set status = 'deleted',
			deleted_at = coalesce(i.deleted_at, now())
		from organizations o
		where i.organization_id = o.id
			and (o.uuid::text = $1 or o.external_id = $1)
			and i.external_id = $2
		returning i.external_id, i.email, i.role, i.status, i.invited_at, i.expires_at
	`, strings.TrimSpace(orgUUID), strings.TrimSpace(inviteID)))
}
