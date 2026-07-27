package db

import (
	"context"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

const (
	consoleInviteColumns = `
		i.external_id AS id,
		i.email,
		i.role,
		i.status,
		i.invited_at,
		i.expires_at
	`
	createConsoleInviteQuery = `
		with org as (
			select id
			from organizations
			where CAST(uuid AS text) = :org_uuid or external_id = :org_uuid
			limit 1
		)
		insert into organization_invites (
			external_id, organization_id, email, role, status, invited_at, expires_at
		)
		select :external_id, org.id, :email, :role, 'pending', :invited_at, :expires_at
		from org
		returning
			external_id AS id,
			email,
			role,
			status,
			invited_at,
			expires_at
	`
	resendConsoleInviteQuery = `
		update organization_invites i
		set status = 'pending',
			invited_at = :invited_at,
			expires_at = :expires_at
		from organizations o
		where i.organization_id = o.id
			and (CAST(o.uuid AS text) = :org_uuid or o.external_id = :org_uuid)
			and i.external_id = :invite_id
			and i.deleted_at is null
		returning ` + consoleInviteColumns + `
	`
	deleteConsoleInviteQuery = `
		update organization_invites i
		set status = 'deleted',
			deleted_at = coalesce(i.deleted_at, now())
		from organizations o
		where i.organization_id = o.id
			and (CAST(o.uuid AS text) = :org_uuid or o.external_id = :org_uuid)
			and i.external_id = :invite_id
		returning ` + consoleInviteColumns + `
	`
)

type consoleInviteRow struct {
	ID        string    `db:"id"`
	Email     string    `db:"email"`
	Role      string    `db:"role"`
	Status    string    `db:"status"`
	InvitedAt time.Time `db:"invited_at"`
	ExpiresAt time.Time `db:"expires_at"`
}

func (d *DB) ListConsoleInvites(ctx context.Context, orgUUID string, status string, limit int) ([]platform.ConsoleInvite, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" {
		return []platform.ConsoleInvite{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	query := `
		select ` + consoleInviteColumns + `
		from organization_invites i
		join organizations o on o.id = i.organization_id
		where (CAST(o.uuid AS text) = :org_uuid or o.external_id = :org_uuid)
	`
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
	query += ` order by i.invited_at desc, i.id desc limit :limit`

	var rows []consoleInviteRow
	err := namedSelectContext(ctx, d.sql, &rows, query, map[string]any{
		"org_uuid": strings.TrimSpace(orgUUID),
		"limit":    limit,
	})
	if err != nil {
		if isUndefinedTableError(err) {
			return []platform.ConsoleInvite{}, nil
		}
		return nil, err
	}

	out := make([]platform.ConsoleInvite, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.invite())
	}
	return out, nil
}

func (d *DB) CreateConsoleInvite(ctx context.Context, input platform.CreateConsoleInviteInput) (platform.ConsoleInvite, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(input.OrgUUID) == "" ||
		strings.TrimSpace(input.Email) == "" || strings.TrimSpace(input.Role) == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	externalID, err := ids.New("invite_")
	if err != nil {
		return platform.ConsoleInvite{}, err
	}
	now := time.Now().UTC()
	invite, err := getConsoleInviteSQLX(ctx, d.sql, createConsoleInviteQuery, map[string]any{
		"org_uuid":    strings.TrimSpace(input.OrgUUID),
		"external_id": externalID,
		"email":       strings.TrimSpace(input.Email),
		"role":        strings.TrimSpace(input.Role),
		"invited_at":  now,
		"expires_at":  now.Add(21 * 24 * time.Hour),
	})
	if isUniqueViolation(err) {
		return platform.ConsoleInvite{}, err
	}
	return invite, err
}

func (d *DB) ResendConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(inviteID) == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	now := time.Now().UTC()
	return getConsoleInviteSQLX(ctx, d.sql, resendConsoleInviteQuery, map[string]any{
		"org_uuid":   strings.TrimSpace(orgUUID),
		"invite_id":  strings.TrimSpace(inviteID),
		"invited_at": now,
		"expires_at": now.Add(21 * 24 * time.Hour),
	})
}

func (d *DB) DeleteConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(inviteID) == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	return getConsoleInviteSQLX(ctx, d.sql, deleteConsoleInviteQuery, map[string]any{
		"org_uuid":  strings.TrimSpace(orgUUID),
		"invite_id": strings.TrimSpace(inviteID),
	})
}

func getConsoleInviteSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (platform.ConsoleInvite, error) {
	var row consoleInviteRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return platform.ConsoleInvite{}, mapNoRows(err)
	}
	return row.invite(), nil
}

func (r consoleInviteRow) invite() platform.ConsoleInvite {
	return platform.ConsoleInvite{
		ID:        r.ID,
		Email:     r.Email,
		Role:      r.Role,
		Status:    r.Status,
		InvitedAt: r.InvitedAt,
		ExpiresAt: r.ExpiresAt,
	}
}
