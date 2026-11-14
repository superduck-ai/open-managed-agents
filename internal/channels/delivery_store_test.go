package channels

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (f *fakeRuntimeStore) PrepareChannelReplyDelivery(_ context.Context, scope db.ChannelDeliveryScope, input db.ChannelReplyDelivery) (db.ChannelReplyDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deliveries == nil {
		f.deliveries = map[string]db.ChannelReplyDelivery{}
	}
	key := scope.ChannelExternalID + ":" + input.InputEventID
	if row, ok := f.deliveries[key]; ok {
		return row, nil
	}
	input.ChannelExternalID = scope.ChannelExternalID
	input.State = "pending"
	f.deliveries[key] = input
	return input, nil
}
func (f *fakeRuntimeStore) ClaimChannelReplyDelivery(_ context.Context, scope db.ChannelDeliveryScope, id, owner string) (db.ChannelReplyDelivery, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := scope.ChannelExternalID + ":" + id
	row, ok := f.deliveries[key]
	if !ok || row.State != "pending" && (row.State != "sent" || !row.LeaseUntil.Before(time.Now())) {
		return db.ChannelReplyDelivery{}, false, nil
	}
	if row.State == "pending" {
		row.State = "sending"
	}
	row.LeaseOwner = owner
	row.LeaseUntil = time.Now().Add(30 * time.Second)
	f.deliveries[key] = row
	return row, true, nil
}
func (f *fakeRuntimeStore) SaveChannelReplyReceipt(_ context.Context, scope db.ChannelDeliveryScope, id, owner, receipt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := scope.ChannelExternalID + ":" + id
	row := f.deliveries[key]
	if row.State != "sending" || row.LeaseOwner != owner || row.LeaseUntil.Before(time.Now()) {
		return db.ErrInvalidState
	}
	row.State = "sent"
	row.ReplyMessageID = receipt
	f.deliveries[key] = row
	return nil
}
func (f *fakeRuntimeStore) RenewChannelReplyDelivery(_ context.Context, scope db.ChannelDeliveryScope, id, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := scope.ChannelExternalID + ":" + id
	row := f.deliveries[key]
	if row.State != "sent" && row.State != "completed" || row.LeaseOwner != owner || row.LeaseUntil.Before(time.Now()) {
		return db.ErrInvalidState
	}
	row.LeaseUntil = time.Now().Add(30 * time.Second)
	f.deliveries[key] = row
	return nil
}

func (f *fakeRuntimeStore) SaveChannelArtifactDelivery(_ context.Context, scope db.ChannelDeliveryScope, id, owner, receipt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := scope.ChannelExternalID + ":" + id
	row := f.deliveries[key]
	if row.State != "sent" || row.LeaseOwner != owner || !row.LeaseUntil.After(time.Now()) {
		return db.ErrInvalidState
	}
	row.ArtifactDelivery = receipt
	row.LeaseUntil = time.Now().Add(30 * time.Second)
	f.deliveries[key] = row
	return nil
}
func (f *fakeRuntimeStore) CompleteChannelReplyDelivery(_ context.Context, scope db.ChannelDeliveryScope, id, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := scope.ChannelExternalID + ":" + id
	row := f.deliveries[key]
	if row.State != "sent" && row.State != "completed" || row.LeaseOwner != owner || row.LeaseUntil.Before(time.Now()) {
		return db.ErrInvalidState
	}
	row.State = "completed"
	f.deliveries[key] = row
	return nil
}
func (f *fakeRuntimeStore) ListChannelReplyDeliveries(_ context.Context, scope db.ChannelDeliveryScope) ([]db.ChannelReplyDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []db.ChannelReplyDelivery
	for _, row := range f.deliveries {
		if row.ChannelExternalID == scope.ChannelExternalID && row.State == "sent" && row.LeaseUntil.Before(time.Now()) {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (f *fakeRuntimeStore) ReleaseChannelReplyDelivery(_ context.Context, scope db.ChannelDeliveryScope, id, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := scope.ChannelExternalID + ":" + id
	row := f.deliveries[key]
	if row.State != "sent" || row.LeaseOwner != owner {
		return db.ErrInvalidState
	}
	row.LeaseUntil = time.Now().Add(-time.Millisecond)
	f.deliveries[key] = row
	return nil
}

func (f *fakeRuntimeStore) NextChannelReplyRecovery(_ context.Context, scope db.ChannelDeliveryScope, owner string) (time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var next time.Time
	for _, row := range f.deliveries {
		if row.ChannelExternalID == scope.ChannelExternalID && row.State == "sent" && row.LeaseOwner != owner && (next.IsZero() || row.LeaseUntil.Before(next)) {
			next = row.LeaseUntil
		}
	}
	return next, !next.IsZero(), nil
}

func (f *fakeRuntimeStore) GetChannelReplyDeliveryByReceipt(_ context.Context, scope db.ChannelDeliveryScope, receipt string) (db.ChannelReplyDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.deliveries {
		if row.ChannelExternalID == scope.ChannelExternalID && row.ReplyMessageID == receipt {
			return row, nil
		}
	}
	return db.ChannelReplyDelivery{}, db.ErrNotFound
}
