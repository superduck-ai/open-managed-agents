package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper BrainWorkerMapper -sql ./brain_worker_mapper.xml -out ./brain_worker_mapper.sqlmap.gen.go -dialect postgres

type recordBrainWorkerParams struct {
	ExternalID        string
	MaxSessions       int
	BusySessions      int
	ResidentSessions  int
	AdmissionFailures int
}

type brainWorkerRow struct {
	CircuitFailures  int        `db:"circuit_failures"`
	CircuitOpenUntil *time.Time `db:"circuit_open_until"`
	Inserted         bool       `db:"inserted"`
}

type updateBrainWorkerCircuitParams struct {
	ExternalID       string
	CircuitFailures  int
	CircuitOpenUntil *time.Time
}

type brainWorkerLoadRow struct {
	MaxSessions      int `db:"max_sessions"`
	BusySessions     int `db:"busy_sessions"`
	ResidentSessions int `db:"resident_sessions"`
}

type BrainWorkerMapper interface {
	Record(ctx context.Context, params recordBrainWorkerParams) (brainWorkerRow, error)
	UpdateCircuit(ctx context.Context, params updateBrainWorkerCircuitParams) (int64, error)
	ListAvailableLoads(ctx context.Context, seenAfter time.Time) ([]brainWorkerLoadRow, error)
	DeleteStale(ctx context.Context, seenBefore time.Time) (int64, error)
}
