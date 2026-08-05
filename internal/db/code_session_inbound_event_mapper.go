package db

import (
	"context"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper CodeSessionInboundEventMapper -sql ./code_session_inbound_event_mapper.xml -dialect postgres

// CodeSessionInboundEventMapper contains queries whose primary table is
// code_session_inbound_events.
type CodeSessionInboundEventMapper interface {
	GetCodeSessionInboundEventByIdempotencyKey(
		ctx context.Context,
		workspaceUUID uuid.UUID,
		idempotencyKey string,
	) (codeSessionEventRow, bool, error)

	InsertCodeSessionInboundEvent(
		ctx context.Context,
		row codeSessionInboundEventInsertRow,
	) (codeSessionEventRow, error)

	ListExistingActivationInboundEvents(
		ctx context.Context,
		organizationUUID uuid.UUID,
		workspaceUUID uuid.UUID,
		idempotencyKeys []string,
	) ([]codeSessionInboundEventIdentityRow, error)

	InsertCodeSessionInboundEvents(
		ctx context.Context,
		rows []codeSessionInboundEventInsertRow,
	) (int64, error)
}
