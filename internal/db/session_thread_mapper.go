package db

import (
	"context"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper SessionThreadMapper -sql ./session_thread_mapper.xml -dialect postgres

// SessionThreadMapper contains queries whose primary table is session_threads.
type SessionThreadMapper interface {
	GetPrimarySessionThread(
		ctx context.Context,
		workspaceUUID uuid.UUID,
		sessionExternalID string,
	) (sessionThreadRow, bool, error)

	GetSessionThreadByExternalID(
		ctx context.Context,
		workspaceUUID uuid.UUID,
		sessionExternalID string,
		threadExternalID string,
	) (sessionThreadRow, bool, error)
}
