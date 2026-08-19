package db

import (
	"bytes"
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper CodeSessionInternalEventMapper -sql ./code_session_internal_event_mapper.xml -out ./code_session_internal_event_mapper.sqlmap.gen.go -dialect postgres

type codeSessionInternalEventRow struct {
	UUID                  string     `db:"uuid"`
	ExternalID            string     `db:"external_id"`
	OrganizationUUID      string     `db:"organization_uuid"`
	WorkspaceUUID         string     `db:"workspace_uuid"`
	CodeSessionUUID       string     `db:"code_session_uuid"`
	CodeSessionExternalID string     `db:"code_session_external_id"`
	SequenceNum           int64      `db:"sequence_num"`
	EventType             string     `db:"event_type"`
	PayloadUUID           string     `db:"payload_uuid"`
	AgentID               *string    `db:"agent_id"`
	IsCompaction          bool       `db:"is_compaction"`
	Payload               []byte     `db:"payload"`
	PayloadHash           string     `db:"payload_hash"`
	IdempotencyKey        string     `db:"idempotency_key"`
	EventMetadata         []byte     `db:"event_metadata"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
	DeletedAt             *time.Time `db:"deleted_at"`
}

type codeSessionInternalEventInsertParams struct {
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionUUID       string
	CodeSessionExternalID string
	SequenceNum           int64
	EventType             string
	PayloadUUID           string
	AgentID               *string
	IsCompaction          bool
	Payload               []byte
	PayloadHash           string
	IdempotencyKey        string
	EventMetadata         []byte
	CreatedAt             time.Time
}

type listCodeSessionInternalEventsParams struct {
	WorkspaceUUID         string
	CodeSessionExternalID string
	Subagents             bool
	AfterSequence         int64
	Limit                 int
}

type CodeSessionInternalEventMapper interface {
	Insert(ctx context.Context, params codeSessionInternalEventInsertParams) (codeSessionInternalEventRow, error)
	ListPage(ctx context.Context, params listCodeSessionInternalEventsParams) ([]codeSessionInternalEventRow, error)
}

func (r codeSessionInternalEventRow) event() CodeSessionInternalEvent {
	return CodeSessionInternalEvent{
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID,
		WorkspaceUUID:         r.WorkspaceUUID,
		CodeSessionUUID:       r.CodeSessionUUID,
		CodeSessionExternalID: r.CodeSessionExternalID,
		SequenceNum:           r.SequenceNum,
		EventType:             r.EventType,
		PayloadUUID:           r.PayloadUUID,
		AgentID:               r.AgentID,
		IsCompaction:          r.IsCompaction,
		Payload:               bytes.Clone(r.Payload),
		PayloadHash:           r.PayloadHash,
		IdempotencyKey:        r.IdempotencyKey,
		EventMetadata:         bytes.Clone(r.EventMetadata),
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
		DeletedAt:             r.DeletedAt,
	}
}
