package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const (
	harnessInputQueued    = "queued"
	harnessInputClaimed   = "claimed"
	harnessInputCancelled = "cancelled"
)

type HarnessInputMessage struct {
	Text            string          `json:"text,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	PayloadBlobUUID *string         `json:"payload_blob_uuid,omitempty"`
	LegacyEvent     json.RawMessage `json:"legacy_event,omitempty"`
}

type HarnessInput struct {
	Ref         HarnessTurnRef
	Message     HarnessInputMessage
	State       string
	PublishedAt *time.Time
}

func (i HarnessInput) PendingPublication() bool {
	return i.State == harnessInputQueued && i.PublishedAt == nil
}

func harnessInputRef(session Session, inputUUID string) HarnessTurnRef {
	return HarnessTurnRef{UUID: inputUUID, SessionUUID: session.UUID, OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID}
}

func enqueueHarnessInputTx(ctx context.Context, mapper HarnessInputMapper, session Session, event SessionEvent) error {
	if session.ExecutionRuntime != sessioncontract.CloudHarnessRuntime || event.EventType != "user.message" {
		return nil
	}
	message := HarnessInputMessage{PayloadBlobUUID: event.PayloadBlobUUID}
	if event.PayloadBlobUUID == nil {
		input, err := sessioncontract.ParseHarnessUserInput(event.Payload)
		if err != nil {
			return err
		}
		message.Text, message.Metadata = input.Text, input.Metadata
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return mapper.Insert(ctx, harnessInputRef(session, event.UUID), string(raw), event.CreatedAt)
}

func (r harnessInputRow) input() (HarnessInput, error) {
	input := HarnessInput{
		Ref:   HarnessTurnRef{UUID: r.InputUUID, SessionUUID: r.SessionUUID, OrganizationUUID: r.OrganizationUUID, WorkspaceUUID: r.WorkspaceUUID},
		State: r.State, PublishedAt: r.PublishedAt,
	}
	err := json.Unmarshal(r.InputMessage, &input.Message)
	return input, err
}

func decodeHarnessInputs(rows []harnessInputRow, err error) ([]HarnessInput, error) {
	if err != nil {
		return nil, err
	}
	inputs := make([]HarnessInput, len(rows))
	for i, row := range rows {
		inputs[i], err = row.input()
		if err != nil {
			return nil, err
		}
	}
	return inputs, nil
}

func (d *DB) FindHarnessInput(ctx context.Context, ref HarnessTurnRef) (HarnessInput, bool, error) {
	row, found, err := NewHarnessInputMapper(d.mapperDB).Find(ctx, ref)
	if err != nil || !found {
		return HarnessInput{}, found, err
	}
	input, err := row.input()
	return input, true, err
}

func (d *DB) ListUnpublishedHarnessInputs(ctx context.Context, limit int) ([]HarnessInput, error) {
	return decodeHarnessInputs(NewHarnessInputMapper(d.mapperDB).ListUnpublished(ctx, limit))
}

func (d *DB) MarkHarnessUserInputPublished(ctx context.Context, ref HarnessTurnRef) error {
	return NewHarnessInputMapper(d.mapperDB).MarkPublished(ctx, ref)
}
