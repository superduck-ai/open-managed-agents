package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper OperationalMetricsMapper -sql ./operational_metrics_mapper.xml -out ./operational_metrics_mapper.sqlmap.gen.go -dialect postgres

type OperationalMetricsMapper interface {
	CountLiveSessionsByStatus(ctx context.Context) ([]operationalStateCountRow, error)
	CountStaleRunningSessions(ctx context.Context, staleBefore time.Time) (int64, error)
	CountActiveCodeSessionsByWorkerState(ctx context.Context) ([]operationalCodeSessionCountRow, error)
	CountLiveEnvironmentWorkByState(ctx context.Context) ([]operationalStateCountRow, error)
	OldestQueuedEnvironmentWorkAgeSeconds(ctx context.Context) (int64, error)
	CountStuckStartingEnvironmentWork(ctx context.Context, startedBefore time.Time) (int64, error)
	CountLiveEnvironmentSandboxesByState(ctx context.Context) ([]operationalStateCountRow, error)
	CountRequestedBrainClaimsByState(ctx context.Context) ([]operationalStateCountRow, error)
	OldestWaitingBrainClaimAgeSeconds(ctx context.Context) (int64, error)
	CountTakeoverPendingBrainClaims(ctx context.Context) (int64, error)
}

type operationalStateCountRow struct {
	State string `db:"state"`
	Total int64  `db:"total"`
}

type operationalCodeSessionCountRow struct {
	WorkerStatus     string `db:"worker_status"`
	ConnectionStatus string `db:"connection_status"`
	Total            int64  `db:"total"`
}
