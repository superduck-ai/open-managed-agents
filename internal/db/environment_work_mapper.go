package db

import (
	"context"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -mapper EnvironmentWorkMapper -sql ./environment_work_mapper.xml -dialect postgres

// EnvironmentWorkMapper contains queries whose primary table is environment_work.
type EnvironmentWorkMapper interface {
	StartupEnvironmentWorkExists(
		ctx context.Context,
		workspaceUUID uuid.UUID,
		environmentUUID uuid.UUID,
		sessionExternalID string,
	) (bool, error)
}
