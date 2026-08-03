package db

import (
	"context"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper SessionEventMapper -sql ./session_event_mapper.xml -dialect postgres

// SessionEventMapper contains queries whose primary table is session_events.
type SessionEventMapper interface {
	InsertSessionEvent(
		ctx context.Context,
		row sessionEventInsertRow,
	) (sessionEventRow, error)

	ListSessionEventsForActivation(
		ctx context.Context,
		organizationUUID uuid.UUID,
		workspaceUUID uuid.UUID,
		sessionUUID uuid.UUID,
	) ([]sessionEventRow, error)

	ListSessionEventsByUUIDs(
		ctx context.Context,
		sessionUUID uuid.UUID,
		sessionEventUUIDs []uuid.UUID,
	) ([]sessionEventRow, error)
}
