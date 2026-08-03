package db

import (
	"context"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper CodeSessionOutboundEventMapper -sql ./code_session_outbound_event_mapper.xml -dialect postgres

// CodeSessionOutboundEventMapper contains queries whose primary table is
// code_session_outbound_events.
type CodeSessionOutboundEventMapper interface {
	GetCodeSessionOutboundEventByIdempotencyKey(
		ctx context.Context,
		workspaceUUID uuid.UUID,
		idempotencyKey string,
	) (codeSessionEventRow, bool, error)

	InsertCodeSessionOutboundEvent(
		ctx context.Context,
		row codeSessionOutboundEventInsertRow,
	) (codeSessionEventRow, error)
}
