package db

import (
	"context"
)

//go:generate go tool sqlmapgen -mapper SessionEventMapper -sql ./session_event_mapper.xml -dialect postgres

// SessionEventMapper contains queries whose primary table is session_events.
type SessionEventMapper interface {
	ListSessionEventsForActivation(
		ctx context.Context,
		organizationUUID string,
		workspaceUUID string,
		sessionUUID string,
	) ([]sessionEventRow, error)
}
