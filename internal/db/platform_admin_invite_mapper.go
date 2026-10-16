package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAdminInviteMapper -sql ./platform_admin_invite_mapper.xml -out ./platform_admin_invite_mapper.sqlmap.gen.go -dialect postgres

type PlatformAdminInvite struct {
	UUID           string    `db:"uuid"`
	ExternalID     string    `db:"external_id"`
	Email          string    `db:"email"`
	Role           string    `db:"role"`
	Status         string    `db:"status"`
	InvitedAt      time.Time `db:"invited_at"`
	ExpiresAt      time.Time `db:"expires_at"`
	DeliveryStatus string    `db:"delivery_status"`
}

type PlatformAdminInviteInput struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	AccountUUID      string
	Email            string
	Role             string
}

type PlatformAdminInviteMapper interface {
	List(ctx context.Context, params PlatformAdminPageQuery) ([]PlatformAdminInvite, error)
	Lock(ctx context.Context, organizationUUID, invitationUUID string) (PlatformAdminInvite, error)
	Insert(ctx context.Context, params PlatformAdminInviteInput) (PlatformAdminInvite, error)
	HasPending(ctx context.Context, organizationUUID, email string) (bool, error)
	Resend(ctx context.Context, organizationUUID, invitationUUID string) error
	Revoke(ctx context.Context, organizationUUID, invitationUUID string) error
	CancelDeliveries(ctx context.Context, organizationUUID, invitationUUID string) error
	Enqueue(ctx context.Context, organizationUUID, invitationUUID string) error
}
