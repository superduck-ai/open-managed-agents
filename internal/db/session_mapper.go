package db

import (
	"context"
)

//go:generate go tool sqlmapgen -mapper SessionMapper -sql ./session_mapper.xml -dialect postgres

// SessionMapper contains queries whose primary table is sessions.
type SessionMapper interface {
	LockSessionForEvents(
		ctx context.Context,
		workspaceUUID string,
		sessionExternalID string,
	) (sessionRow, bool, error)
}
