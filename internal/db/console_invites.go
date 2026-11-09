package db

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/yourbatis"
)

func (d *DB) ListConsoleInvites(ctx context.Context, orgUUID string, status string, limit int) ([]platform.ConsoleInvite, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" {
		return []platform.ConsoleInvite{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	switch status {
	case "", "pending", "expired", "accepted", "deleted":
	default:
		return []platform.ConsoleInvite{}, nil
	}
	mapper := NewConsoleInviteMapper(d.mapperDB)
	rows, err := mapper.List(ctx, orgUUID, status, limit)
	if err != nil {
		return nil, err
	}

	out := make([]platform.ConsoleInvite, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.invite())
	}
	return out, nil
}

func (d *DB) CreateConsoleInvite(ctx context.Context, input platform.CreateConsoleInviteInput) (platform.ConsoleInvite, error) {
	if d == nil || d.mapperDB == nil || input.OrgUUID == "" || input.Email == "" || input.Role == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	externalID, err := ids.New("invite_")
	if err != nil {
		return platform.ConsoleInvite{}, err
	}
	now := time.Now().UTC()
	mapper := NewConsoleInviteMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, insertConsoleInviteParams{
		ExternalID:       externalID,
		OrganizationUUID: input.OrgUUID,
		Email:            input.Email,
		Role:             input.Role,
		InvitedAt:        now,
		ExpiresAt:        now.Add(21 * 24 * time.Hour),
	})
	if err != nil {
		return platform.ConsoleInvite{}, err
	}
	return row.invite(), nil
}

func (d *DB) ResendConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" || inviteID == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	now := time.Now().UTC()
	var row consoleInviteRow
	err := d.withInvitationOrganizationLock(ctx, orgUUID, func(executor yourbatis.Executor) error {
		var err error
		row, err = NewConsoleInviteMapper(executor).ResendByExternalID(ctx, resendConsoleInviteParams{
			OrganizationUUID: orgUUID,
			ExternalID:       inviteID,
			InvitedAt:        now,
			ExpiresAt:        now.Add(21 * 24 * time.Hour),
		})
		return err
	})
	if err != nil {
		return platform.ConsoleInvite{}, mapNoRows(err)
	}
	return row.invite(), nil
}

func (d *DB) DeleteConsoleInvite(ctx context.Context, orgUUID string, inviteID string) (platform.ConsoleInvite, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" || inviteID == "" {
		return platform.ConsoleInvite{}, platform.ErrNotFound
	}
	var row consoleInviteRow
	err := d.withInvitationOrganizationLock(ctx, orgUUID, func(executor yourbatis.Executor) error {
		var err error
		row, err = NewConsoleInviteMapper(executor).SoftDeleteByExternalID(ctx, orgUUID, inviteID)
		return err
	})
	if err != nil {
		return platform.ConsoleInvite{}, mapNoRows(err)
	}
	return row.invite(), nil
}

func (d *DB) withInvitationOrganizationLock(ctx context.Context, org string, change func(yourbatis.Executor) error) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if _, err := NewOrganizationMemberMapper(executor).LockOrganization(ctx, org); err != nil {
			return mapNoRows(err)
		}
		return change(executor)
	})
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
