package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminInvite struct {
	UUID             uuid.UUID `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
	Email            string    `db:"email"`
	Role             string    `db:"role"`
	Status           string    `db:"status"`
	InvitedAt        time.Time `db:"invited_at"`
	ExpiresAt        time.Time `db:"expires_at"`
}

type ListAdminInvitesParams struct {
	OrganizationUUID string
	AfterID          string
	BeforeID         string
	Limit            int
}

func (d *DB) CreateAdminInvite(ctx context.Context, invite AdminInvite) (AdminInvite, error) {
	created, err := getAdminRow[AdminInvite](ctx, d.sql, `
		insert into organization_invites (
			external_id, organization_uuid, email, role, status, invited_at, expires_at
		)
		values (
			:external_id, :organization_uuid, :email, :role, :status, :invited_at, :expires_at
		)
		returning uuid, external_id,
			organization_uuid,
			email, role, status, invited_at, expires_at
	`, adminInviteArguments(invite))
	if isUniqueViolation(err) {
		return AdminInvite{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminInvite(ctx context.Context, organizationUUID, externalID string) (AdminInvite, error) {
	return getAdminRow[AdminInvite](ctx, d.sql, adminInviteSelectSQL()+`
		where organization_uuid = :organization_uuid and external_id = :external_id
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
}

func (d *DB) ListAdminInvitesPage(ctx context.Context, params ListAdminInvitesParams) ([]AdminInvite, bool, error) {
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	cursor, cursorOK, err := d.adminCursor(
		ctx,
		"organization_invites",
		"invited_at",
		"organization_uuid = :organization_uuid and external_id = :cursor_external_id",
		map[string]any{"organization_uuid": dbUUID(params.OrganizationUUID), "cursor_external_id": cursorID},
		cursorID,
	)
	if err != nil {
		return nil, false, err
	}
	if (params.AfterID != "" || params.BeforeID != "") && !cursorOK {
		return nil, false, nil
	}
	query := adminInviteSelectSQL() + ` where organization_uuid = :organization_uuid`
	args := map[string]any{"organization_uuid": dbUUID(params.OrganizationUUID), "limit": params.Limit + 1}
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
		where organization_uuid = :organization_uuid and external_id = :external_id
		returning uuid, external_id,
			organization_uuid,
			email, role, status, invited_at, expires_at
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
}

func adminInviteSelectSQL() string {
	return `
		select uuid, external_id,
			organization_uuid,
			email, role, status, invited_at, expires_at
		from organization_invites
	`
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
