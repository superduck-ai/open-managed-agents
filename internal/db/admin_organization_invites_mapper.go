package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper AdminInviteMapper -sql ./admin_organization_invites_mapper.xml -out ./admin_organization_invites_mapper.sqlmap.gen.go -dialect postgres

type insertAdminInviteParams struct {
	ExternalID       string
	OrganizationUUID uuid.UUID
	Email            string
	Role             string
	Status           string
	InvitedAt        time.Time
	ExpiresAt        time.Time
}

type AdminInviteMapper interface {
	Insert(ctx context.Context, params insertAdminInviteParams) (AdminInvite, error)
	FindByExternalID(ctx context.Context, organizationUUID, externalID string) (AdminInvite, error)
	FindPageAnchorByExternalID(ctx context.Context, organizationUUID, externalID string) (pagePosition, bool, error)
	ListPage(ctx context.Context, organizationUUID string,
		anchor *pagePosition, before bool, limit int) ([]AdminInvite, error)
	SoftDeleteByExternalID(ctx context.Context, organizationUUID, externalID string) (AdminInvite, error)
}
