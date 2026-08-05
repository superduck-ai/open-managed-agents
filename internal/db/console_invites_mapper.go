package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ConsoleInviteMapper -sql ./console_invites_mapper.xml -out ./console_invites_mapper.sqlmap.gen.go -dialect postgres

type consoleInviteRow struct {
	ID        string    `db:"id"`
	Email     string    `db:"email"`
	Role      string    `db:"role"`
	Status    string    `db:"status"`
	InvitedAt time.Time `db:"invited_at"`
	ExpiresAt time.Time `db:"expires_at"`
}

type insertConsoleInviteParams struct {
	ExternalID       string
	OrganizationUUID string
	Email            string
	Role             string
	InvitedAt        time.Time
	ExpiresAt        time.Time
}

type resendConsoleInviteParams struct {
	OrganizationUUID string
	ExternalID       string
	InvitedAt        time.Time
	ExpiresAt        time.Time
}

type ConsoleInviteMapper interface {
	List(ctx context.Context, organizationUUID, status string, limit int) ([]consoleInviteRow, error)
	Insert(ctx context.Context, params insertConsoleInviteParams) (consoleInviteRow, error)
	ResendByExternalID(ctx context.Context, params resendConsoleInviteParams) (consoleInviteRow, error)
	SoftDeleteByExternalID(ctx context.Context, organizationUUID, externalID string) (consoleInviteRow, error)
}
