package db

import (
	"bytes"
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper CodeSessionInboundEventMapper -sql ./code_session_inbound_event_mapper.xml -out ./code_session_inbound_event_mapper.sqlmap.gen.go -dialect postgres

type codeSessionEventRow struct {
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	CodeSessionUUID       string     `db:"code_session_uuid"`
	CodeSessionExternalID string     `db:"code_session_external_id"`
	SequenceNum           int64      `db:"sequence_num"`
	EventType             string     `db:"event_type"`
	EventSubtype          string     `db:"event_subtype"`
	PayloadUUID           *string    `db:"payload_uuid"`
	RequestID             *string    `db:"request_id"`
	Payload               []byte     `db:"payload"`
	PayloadHash           string     `db:"payload_hash"`
	IdempotencyKey        string     `db:"idempotency_key"`
	DeliveryStatus        string     `db:"delivery_status"`
	Source                string     `db:"source"`
	SentAt                *time.Time `db:"sent_at"`
	DeliveryWorkerEpoch   *int64     `db:"delivery_worker_epoch"`
	ReceivedAt            *time.Time `db:"received_at"`
	ProcessingAt          *time.Time `db:"processing_at"`
	ProcessedAt           *time.Time `db:"processed_at"`
	LastDeliveryAttemptAt *time.Time `db:"last_delivery_attempt_at"`
	LastDeliveryUpdateAt  *time.Time `db:"last_delivery_update_at"`
	DeliveryAttempts      int        `db:"delivery_attempts"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

type codeSessionInboundEventIdentityRow struct {
	CodeSessionExternalID string `db:"code_session_external_id"`
	IdempotencyKey        string `db:"idempotency_key"`
}

type codeSessionInboundEventInsertRow struct {
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionUUID       string
	CodeSessionExternalID string
	SequenceNum           int64
	EventType             string
	EventSubtype          string
	PayloadUUID           *string
	RequestID             *string
	Payload               []byte
	PayloadHash           string
	IdempotencyKey        string
	DeliveryStatus        string
	Source                string
	CreatedAt             time.Time
}

type updateCodeSessionInboundDeliveryParams struct {
	UUID           string
	TargetStatus   string
	MarkReceived   bool
	MarkProcessing bool
	MarkProcessed  bool
	Epoch          int64
	Now            time.Time
}

// CodeSessionInboundEventMapper contains queries whose primary table is code_session_inbound_events.
type CodeSessionInboundEventMapper interface {
	GetCodeSessionInboundEventByIdempotencyKey(ctx context.Context, workspaceUUID, idempotencyKey string) (codeSessionEventRow, bool, error)
	InsertCodeSessionInboundEvent(ctx context.Context, row codeSessionInboundEventInsertRow) (codeSessionEventRow, error)
	ListExistingActivationInboundEvents(ctx context.Context, organizationUUID, workspaceUUID string, idempotencyKeys []string) ([]codeSessionInboundEventIdentityRow, error)
	InsertCodeSessionInboundEvents(ctx context.Context, rows []codeSessionInboundEventInsertRow) (int64, error)
	ListQueued(ctx context.Context, codeSessionExternalID string) ([]codeSessionEventRow, error)
	ListQueuedForEpoch(ctx context.Context, codeSessionExternalID string, epoch int64) ([]codeSessionEventRow, error)
	ListForWorkerStream(ctx context.Context, codeSessionExternalID string, epoch, afterSequence int64) ([]codeSessionEventRow, error)
	MarkSent(ctx context.Context, eventExternalID string) (int64, error)
	MarkSentForEpoch(ctx context.Context, codeSessionExternalID, eventExternalID string, epoch int64) (int64, error)
	LockDeliveryByPayloadUUID(ctx context.Context, codeSessionUUID, eventID string) (codeSessionEventRow, error)
	LockDeliveryByExternalID(ctx context.Context, codeSessionUUID, eventID string) (codeSessionEventRow, error)
	UpdateDelivery(ctx context.Context, params updateCodeSessionInboundDeliveryParams) error
}

func (r codeSessionEventRow) event() CodeSessionEvent {
	return CodeSessionEvent{
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID,
		WorkspaceUUID:         r.WorkspaceUUID,
		CodeSessionUUID:       r.CodeSessionUUID,
		CodeSessionExternalID: r.CodeSessionExternalID,
		SequenceNum:           r.SequenceNum,
		EventType:             r.EventType,
		EventSubtype:          r.EventSubtype,
		PayloadUUID:           r.PayloadUUID,
		RequestID:             r.RequestID,
		Payload:               bytes.Clone(r.Payload),
		PayloadHash:           r.PayloadHash,
		IdempotencyKey:        r.IdempotencyKey,
		DeliveryStatus:        r.DeliveryStatus,
		Source:                r.Source,
		SentAt:                r.SentAt,
		DeliveryWorkerEpoch:   r.DeliveryWorkerEpoch,
		ReceivedAt:            r.ReceivedAt,
		ProcessingAt:          r.ProcessingAt,
		ProcessedAt:           r.ProcessedAt,
		LastDeliveryAttemptAt: r.LastDeliveryAttemptAt,
		LastDeliveryUpdateAt:  r.LastDeliveryUpdateAt,
		DeliveryAttempts:      r.DeliveryAttempts,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		DeletedAt:             r.DeletedAt,
	}
}
