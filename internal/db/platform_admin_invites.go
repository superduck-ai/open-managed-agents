package db

import "context"

func (d *DB) ListPlatformAdminInvites(ctx context.Context, query PlatformAdminPageQuery) ([]PlatformAdminInvite, error) {
	return NewPlatformAdminInviteMapper(d.mapperDB).List(ctx, query)
}

func (tx *PlatformAdminTx) Invite(ctx context.Context, org, invitation string) (PlatformAdminInvite, error) {
	row, err := NewPlatformAdminInviteMapper(tx.executor).Lock(ctx, org, invitation)
	return row, mapNoRows(err)
}

func (tx *PlatformAdminTx) HasPendingInvite(ctx context.Context, org, email string) (bool, error) {
	return NewPlatformAdminInviteMapper(tx.executor).HasPending(ctx, org, email)
}

func (tx *PlatformAdminTx) InsertInvite(ctx context.Context, input PlatformAdminInviteInput) (PlatformAdminInvite, error) {
	mapper := NewPlatformAdminInviteMapper(tx.executor)
	row, err := mapper.Insert(ctx, input)
	if err != nil {
		return row, err
	}
	return row, mapper.Enqueue(ctx, input.OrganizationUUID, row.UUID)
}

func (tx *PlatformAdminTx) ResendInvite(ctx context.Context, org, invitation string) error {
	mapper := NewPlatformAdminInviteMapper(tx.executor)
	if err := mapper.CancelDeliveries(ctx, org, invitation); err != nil {
		return err
	}
	if err := mapper.Resend(ctx, org, invitation); err != nil {
		return err
	}
	return mapper.Enqueue(ctx, org, invitation)
}

func (tx *PlatformAdminTx) RevokeInvite(ctx context.Context, org, invitation string) error {
	mapper := NewPlatformAdminInviteMapper(tx.executor)
	if err := mapper.Revoke(ctx, org, invitation); err != nil {
		return err
	}
	return mapper.CancelDeliveries(ctx, org, invitation)
}
