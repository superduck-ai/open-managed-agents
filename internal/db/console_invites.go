package db

import (
	"context"
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
		insert into organization_invites (
			external_id, organization_uuid, email, role, status, invited_at, expires_at
		)
		values (
			:external_id, :org_uuid, :email, :role,
			'pending', :invited_at, :expires_at
		)
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
		where i.organization_uuid = :org_uuid
			and i.external_id = :invite_id
			and i.deleted_at is null
		returning ` + consoleInviteColumns + `
	`
	deleteConsoleInviteQuery = `
		update organization_invites i
		set status = 'deleted',
			deleted_at = coalesce(i.deleted_at, now())
		where i.organization_uuid = :org_uuid
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
	if d == nil || d.sql == nil || orgUUID == "" {
		return []platform.ConsoleInvite{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	query := `
		select ` + consoleInviteColumns + `
		from organization_invites i
		where i.organization_uuid = :org_uuid
	`
	switch status {
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
	query += ` order by i.invited_at desc, i.uuid desc limit :limit`

	var rows []consoleInviteRow
	err := namedSelectContext(ctx, d.sql, &rows, query, map[string]any{
		"org_uuid": orgUUID,
		"limit":    limit,
	})
	if err != nil {
		if isUndefinedRelationError(err) {
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
	if d == nil || d.sql == nil || input.OrgUUID == "" || input.Email == "" || input.Role == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	externalID, err := ids.New("invite_")
	if err != nil {
		return platform.ConsoleInvite{}, err
	}
	now := time.Now().UTC()
	invite, err := getConsoleInviteSQLX(ctx, d.sql, createConsoleInviteQuery, map[string]any{
		"org_uuid":    input.OrgUUID,
		"external_id": externalID,
		"email":       input.Email,
		"role":        input.Role,
		"invited_at":  now,
		"expires_at":  now.Add(21 * 24 * time.Hour),
	})
	if isUniqueViolation(err) {
		return platform.ConsoleInvite{}, err
	}
	return invite, err
}

func (d *DB) ResendConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.sql == nil || orgUUID == "" || inviteID == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	now := time.Now().UTC()
	return getConsoleInviteSQLX(ctx, d.sql, resendConsoleInviteQuery, map[string]any{
		"org_uuid":   orgUUID,
		"invite_id":  inviteID,
		"invited_at": now,
		"expires_at": now.Add(21 * 24 * time.Hour),
	})
}

func (d *DB) DeleteConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.sql == nil || orgUUID == "" || inviteID == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	return getConsoleInviteSQLX(ctx, d.sql, deleteConsoleInviteQuery, map[string]any{
		"org_uuid":  orgUUID,
		"invite_id": inviteID,
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
