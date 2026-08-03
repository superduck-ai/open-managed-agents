package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper CodeSessionMapper -sql ./code_session_mapper.xml -dialect postgres

// CodeSessionMapper contains queries whose primary table is code_sessions.
type CodeSessionMapper interface {
	LockCodeSessionByExternalID(
		ctx context.Context,
		codeSessionExternalID string,
	) (codeSessionRow, bool, error)

	LockInitializingCodeSession(
		ctx context.Context,
		codeSessionUUID uuid.UUID,
	) (codeSessionRow, bool, error)

	UpdateCodeSessionInboundSequence(
		ctx context.Context,
		codeSessionUUID uuid.UUID,
		sequenceNum int64,
		now time.Time,
	) (int64, error)

	UpdateCodeSessionOutboundSequence(
		ctx context.Context,
		codeSessionUUID uuid.UUID,
		sequenceNum int64,
		now time.Time,
	) (int64, error)

	ActivateCodeSession(
		ctx context.Context,
		codeSessionUUID uuid.UUID,
		now time.Time,
	) (int64, error)

	GetLatestCodeSessionStatus(
		ctx context.Context,
		sessionUUID uuid.UUID,
	) (string, bool, error)
}
