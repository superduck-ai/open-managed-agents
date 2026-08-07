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
	mapper := NewAdminInviteMapper(d.mapperDB)
	created, err := mapper.Insert(ctx, insertAdminInviteParams{
		ExternalID:       invite.ExternalID,
		OrganizationUUID: invite.OrganizationUUID.String(),
		Email:            invite.Email,
		Role:             invite.Role,
		Status:           invite.Status,
		InvitedAt:        invite.InvitedAt,
		ExpiresAt:        invite.ExpiresAt,
	})
	if isUniqueViolation(err) {
		return AdminInvite{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminInvite(ctx context.Context, organizationUUID, externalID string) (AdminInvite, error) {
	mapper := NewAdminInviteMapper(d.mapperDB)
	invite, err := mapper.FindByExternalID(ctx, organizationUUID, externalID)
	return invite, mapNoRows(err)
}

func (d *DB) ListAdminInvitesPage(ctx context.Context, params ListAdminInvitesParams) ([]AdminInvite, bool, error) {
	mapper := NewAdminInviteMapper(d.mapperDB)
	var anchor *pagePosition
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	if cursorID != "" {
		value, found, err := mapper.FindPageAnchorByExternalID(ctx, params.OrganizationUUID, cursorID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil
		}
		anchor = &value
	}
	before := params.AfterID == "" && params.BeforeID != ""
	invites, err := mapper.ListPage(ctx, params.OrganizationUUID, anchor, before, params.Limit+1)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(invites, params.Limit), len(invites) > params.Limit, nil
}

func (d *DB) DeleteAdminInvite(ctx context.Context, organizationUUID, externalID string) (AdminInvite, error) {
	mapper := NewAdminInviteMapper(d.mapperDB)
	invite, err := mapper.SoftDeleteByExternalID(ctx, organizationUUID, externalID)
	return invite, mapNoRows(err)
}
