package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper InvitationMapper -sql ./invitation.xml -out ./invitation.sqlmap.gen.go -dialect postgres

type invitationRow struct {
	ID               string    `db:"id"`
	OrganizationUUID string    `db:"organization_uuid"`
	OrganizationName string    `db:"organization_name"`
	Email            string    `db:"email"`
	Role             string    `db:"role"`
	Status           string    `db:"status"`
	InvitedAt        time.Time `db:"invited_at"`
	ExpiresAt        time.Time `db:"expires_at"`
}

type invitationMemberRow struct {
	ID   string `db:"id"`
	Role string `db:"role"`
}

type invitationMemberParams struct {
	ID               string
	OrganizationUUID string
	Email            string
	Role             string
}

type InvitationMapper interface {
	ListByEmail(ctx context.Context, email string) ([]invitationRow, error)
	LockByIDAndEmail(ctx context.Context, id, email string) (invitationRow, bool, error)
	FindActiveMember(ctx context.Context, organizationUUID, email string) (invitationMemberRow, bool, error)
	InsertMember(ctx context.Context, params invitationMemberParams) (int64, error)
	UpdateStatus(ctx context.Context, organizationUUID, id, status string) (int64, error)
}
