package db

import (
	"context"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper SessionMapper -sql ./session_mapper.xml -dialect postgres

// SessionMapper contains queries whose primary table is sessions.
type SessionMapper interface {
	LockSessionForEvents(
		ctx context.Context,
		workspaceUUID uuid.UUID,
		sessionExternalID string,
	) (sessionRow, bool, error)

	SetSessionOutcomeEvaluations(
		ctx context.Context,
		workspaceUUID uuid.UUID,
		sessionExternalID string,
		outcomeEvaluations []byte,
	) (int64, error)
}
