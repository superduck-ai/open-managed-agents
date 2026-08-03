package db

import (
	"context"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper SessionEventQueueMapper -sql ./session_event_queue_mapper.xml -dialect postgres

// SessionEventQueueMapper contains queries whose primary table is
// session_event_queue.
type SessionEventQueueMapper interface {
	ListSessionEventQueueIdentities(
		ctx context.Context,
		sessionUUID uuid.UUID,
	) ([]sessionEventQueueIdentityRow, error)

	DeleteSessionEventQueue(
		ctx context.Context,
		sessionUUID uuid.UUID,
	) (int64, error)

	SessionEventQueueExists(
		ctx context.Context,
		sessionUUID uuid.UUID,
	) (bool, error)

	EnqueueSessionEvents(
		ctx context.Context,
		rows []sessionEventQueueInsertRow,
	) (int64, error)
}
