package db

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"time"
	"uuid"
)

type ChannelDeliveryScope struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	ChannelExternalID string
}

type ChannelReplyDelivery struct {
	UUID              string    `db:"uuid"`
	ExternalID        string    `db:"external_id"`
	ChannelUUID       string    `db:"channel_uuid"`
	ArtifactDelivery  string    `db:"artifact_delivery"`
	ChannelType       string    `db:"channel_type"`
	AccountID         string    `db:"account_id"`
	ChannelExternalID string    `db:"channel_external_id"`
	InputEventID      string    `db:"input_event_id"`
	SessionExternalID string    `db:"session_external_id"`
	ScopeKey          string    `db:"scope_key"`
	MessageID         string    `db:"message_id"`
	ChatID            string    `db:"chat_id"`
	ChatType          string    `db:"chat_type"`
	ActorID           string    `db:"actor_id"`
	RootID            string    `db:"root_id"`
	ThreadID          string    `db:"thread_id"`
	ReplyMessageID    string    `db:"reply_message_id"`
	IncludeThinking   bool      `db:"include_thinking"`
	IncludeToolCalls  bool      `db:"include_tool_calls"`
	State             string    `db:"state"`
	LeaseOwner        string    `db:"lease_owner"`
	LeaseUntil        time.Time `db:"lease_until"`
}

func (d *DB) SaveChannelArtifactDelivery(ctx context.Context, scope ChannelDeliveryScope, inputID, owner, receipt string) error {
	_, found, err := NewChannelRecordMapper(d.mapperDB).SaveArtifactDelivery(ctx, scope, inputID, owner, receipt)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) PrepareChannelReplyDelivery(ctx context.Context, scope ChannelDeliveryScope, input ChannelReplyDelivery) (ChannelReplyDelivery, error) {
	var err error
	input.UUID = uuid.NewV4().String()
	input.ExternalID, err = ids.New("crdl_")
	if err != nil {
		return ChannelReplyDelivery{}, err
	}
	mapper := NewChannelRecordMapper(d.mapperDB)
	row, found, err := mapper.InsertReplyDelivery(ctx, scope, input)
	if err != nil {
		return ChannelReplyDelivery{}, err
	}
	if found {
		return row, nil
	}
	row, err = mapper.FindReplyDelivery(ctx, scope, input.InputEventID)
	return row, mapNoRows(err)
}

func (d *DB) RetireChannelReplyDelivery(ctx context.Context, scope ChannelDeliveryScope, inputID, owner string) error {
	_, _, err := NewChannelRecordMapper(d.mapperDB).RetireReplyDelivery(ctx, scope, inputID, owner)
	return err
}

func (d *DB) ClaimChannelReplyDelivery(ctx context.Context, scope ChannelDeliveryScope, inputID, owner string) (ChannelReplyDelivery, bool, error) {
	return NewChannelRecordMapper(d.mapperDB).ClaimReplyDelivery(ctx, scope, inputID, owner)
}

func (d *DB) SaveChannelReplyReceipt(ctx context.Context, scope ChannelDeliveryScope, inputID, owner, receipt string) error {
	_, found, err := NewChannelRecordMapper(d.mapperDB).SaveReplyReceipt(ctx, scope, inputID, owner, receipt)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) RenewChannelReplyDelivery(ctx context.Context, scope ChannelDeliveryScope, inputID, owner string) error {
	_, found, err := NewChannelRecordMapper(d.mapperDB).RenewReplyDelivery(ctx, scope, inputID, owner)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) CompleteChannelReplyDelivery(ctx context.Context, scope ChannelDeliveryScope, inputID, owner string) error {
	_, found, err := NewChannelRecordMapper(d.mapperDB).CompleteReplyDelivery(ctx, scope, inputID, owner)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) ListChannelReplyDeliveries(ctx context.Context, scope ChannelDeliveryScope) ([]ChannelReplyDelivery, error) {
	return NewChannelRecordMapper(d.mapperDB).ListReplyDeliveries(ctx, scope)
}

func (d *DB) ReleaseChannelReplyDelivery(ctx context.Context, scope ChannelDeliveryScope, inputID, owner string) error {
	_, found, err := NewChannelRecordMapper(d.mapperDB).ReleaseReplyDelivery(ctx, scope, inputID, owner)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidState
	}
	return nil
}

func (d *DB) NextChannelReplyRecovery(ctx context.Context, scope ChannelDeliveryScope, owner string) (time.Time, bool, error) {
	return NewChannelRecordMapper(d.mapperDB).NextReplyRecovery(ctx, scope, owner)
}

func (d *DB) GetChannelReplyDeliveryByReceipt(ctx context.Context, scope ChannelDeliveryScope, receipt string) (ChannelReplyDelivery, error) {
	row, found, err := NewChannelRecordMapper(d.mapperDB).FindReplyDeliveryByReceipt(ctx, scope, receipt)
	if err != nil {
		return ChannelReplyDelivery{}, err
	}
	if !found {
		return ChannelReplyDelivery{}, ErrNotFound
	}
	return row, nil
}
