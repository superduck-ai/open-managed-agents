package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper EnvironmentWorkerPollMapper -sql ./environment_worker_poll_mapper.xml -out ./environment_worker_poll_mapper.sqlmap.gen.go -dialect postgres

type EnvironmentWorkerPollMapper interface {
	Upsert(ctx context.Context, workspaceUUID, environmentExternalID, workerID string) error
}
