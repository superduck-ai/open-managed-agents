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
	return NewPlatformAdminInviteMapper(tx.executor).Insert(ctx, input)
}

func (tx *PlatformAdminTx) ResendInvite(ctx context.Context, org, invitation string) error {
	return NewPlatformAdminInviteMapper(tx.executor).Resend(ctx, org, invitation)
}

func (tx *PlatformAdminTx) RevokeInvite(ctx context.Context, org, invitation string) error {
	return NewPlatformAdminInviteMapper(tx.executor).Revoke(ctx, org, invitation)
}

func (tx *PlatformAdminTx) EnqueueInviteDelivery(ctx context.Context, org, invitation string) (PlatformAdminInvite, error) {
	row, err := NewPlatformAdminInviteMapper(tx.executor).Enqueue(ctx, org, invitation)
	return row, mapNoRows(err)
}
