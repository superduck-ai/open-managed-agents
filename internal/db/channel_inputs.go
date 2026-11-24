package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type ChannelInputScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	ChannelUUID      string
	ChannelType      string
	AccountID        string
}

type ChannelInput struct {
	UUID              string    `db:"uuid"`
	MessageID         string    `db:"message_id"`
	ScopeKey          string    `db:"scope_key"`
	Payload           string    `db:"payload"`
	State             string    `db:"state"`
	SessionExternalID string    `db:"session_external_id"`
	PreparedContent   string    `db:"prepared_content"`
	InputEventID      string    `db:"input_event_id"`
	LeaseOwner        string    `db:"lease_owner"`
	LeaseUntil        time.Time `db:"lease_until"`
}

type ChannelPairingResume struct {
	MessageID string
	ScopeKey  string
	ActorID   string
	PairingID string
}

type ChannelInputBatch struct {
	MessageIDs []string
	ScopeKey   string
	Payload    string
}

type ChannelInputAdvance struct {
	State             string
	SessionExternalID string
	PreparedContent   string
	InputEventID      string
}

func (d *DB) ReceiveChannelInput(ctx context.Context, scope ChannelInputScope, input ChannelInput) (ChannelInput, bool, error) {
	return NewChannelInputMapper(d.mapperDB).Insert(ctx, scope, input)
}

func (d *DB) ClaimChannelInput(ctx context.Context, scope ChannelInputScope, inputUUID, owner string) (ChannelInput, bool, error) {
	return NewChannelInputMapper(d.mapperDB).Claim(ctx, scope, inputUUID, owner)
}

func (d *DB) ListRecoverableChannelInputs(ctx context.Context, scope ChannelInputScope) ([]ChannelInput, error) {
	return NewChannelInputMapper(d.mapperDB).ListRecoverable(ctx, scope)
}

func (d *DB) HoldPairingOAuth(ctx context.Context, scope ChannelInputScope, inputUUID, owner, patch string) error {
	n, err := NewChannelInputMapper(d.mapperDB).HoldOAuth(ctx, scope, inputUUID, owner, patch)
	if err == nil && n != 1 {
		return ErrInvalidState
	}
	return err
}
func (d *DB) PairingOAuthInput(ctx context.Context, scope ChannelInputScope, id string) (ChannelInput, error) {
	row, ok, err := NewChannelInputMapper(d.mapperDB).PairingInput(ctx, scope, id)
	if err == nil && !ok {
		return row, ErrNotFound
	}
	return row, err
}
func (d *DB) HasWaitingChannelPairingInput(ctx context.Context, scope ChannelInputScope, input ChannelPairingResume) (bool, error) {
	return NewChannelInputMapper(d.mapperDB).HasPairingWait(ctx, scope, input)
}

func (d *DB) ResumeChannelInputAfterPairing(ctx context.Context, scope ChannelInputScope, input ChannelPairingResume) (string, bool, error) {
	return NewChannelInputMapper(d.mapperDB).ResumeAfterPairing(ctx, scope, input)
}

func (d *DB) ResumeOAuthWaiters(ctx context.Context, scope ChannelInputScope, input ChannelPairingResume, provider string) ([]ChannelInput, error) {
	return NewChannelInputMapper(d.mapperDB).ResumeOAuthWaiters(ctx, scope, input, provider)
}

func (d *DB) PrepareChannelInputBatch(ctx context.Context, scope ChannelInputScope, owner string, batch ChannelInputBatch) (ChannelInput, error) {
	if len(batch.MessageIDs) == 0 {
		return ChannelInput{}, ErrInvalidState
	}
	var result ChannelInput
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewChannelInputMapper(executor)
		rows, err := mapper.LockSources(ctx, scope, owner, batch)
		if err != nil {
			return err
		}
		if len(rows) != len(batch.MessageIDs) {
			return ErrInvalidState
		}
		for _, row := range rows {
			if row.MessageID == batch.MessageIDs[len(batch.MessageIDs)-1] {
				result = row
			}
		}
		if err := mapper.GroupSources(ctx, scope, owner, result.UUID, batch); err != nil {
			return err
		}
		row, found, err := mapper.Prepare(ctx, scope, result.UUID, owner, batch)
		if err != nil {
			return err
		}
		if !found {
			return ErrInvalidState
		}
		result = row
		return nil
	})
	return result, err
}

func (d *DB) AdvanceChannelInput(ctx context.Context, scope ChannelInputScope, inputUUID, owner string, change ChannelInputAdvance) (ChannelInput, error) {
	row, found, err := NewChannelInputMapper(d.mapperDB).Advance(ctx, scope, inputUUID, owner, change)
	if err != nil {
		return ChannelInput{}, err
	}
	if !found {
		return ChannelInput{}, ErrInvalidState
	}
	return row, nil
}

func (d *DB) CompleteChannelInput(ctx context.Context, scope ChannelInputScope, inputUUID, owner string) error {
	_, found, err := NewChannelInputMapper(d.mapperDB).Complete(ctx, scope, inputUUID, owner)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) ReleaseChannelInput(ctx context.Context, scope ChannelInputScope, inputUUID, owner string) error {
	return NewChannelInputMapper(d.mapperDB).Release(ctx, scope, inputUUID, owner)
}

func (d *DB) RenewChannelInputs(ctx context.Context, scope ChannelInputScope, owner string) error {
	return NewChannelInputMapper(d.mapperDB).Renew(ctx, scope, owner)
}
