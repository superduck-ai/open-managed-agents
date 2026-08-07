package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper CodeSessionOutboundEventMapper -sql ./code_session_outbound_event_mapper.xml -out ./code_session_outbound_event_mapper.sqlmap.gen.go -dialect postgres

type codeSessionOutboundEventInsertRow struct {
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
	Source                string
	Ephemeral             bool
	CreatedAt             time.Time
}

// CodeSessionOutboundEventMapper contains queries whose primary table is code_session_outbound_events.
type CodeSessionOutboundEventMapper interface {
	GetCodeSessionOutboundEventByIdempotencyKey(ctx context.Context, workspaceUUID, idempotencyKey string) (codeSessionEventRow, bool, error)
	InsertCodeSessionOutboundEvent(ctx context.Context, row codeSessionOutboundEventInsertRow) (codeSessionEventRow, error)
	ListAfter(ctx context.Context, codeSessionExternalID string, afterSequence int64, limit int) ([]codeSessionEventRow, error)
	FindLatestToolPermissionRequest(ctx context.Context, codeSessionExternalID, toolUseID string) (codeSessionEventRow, error)
}
