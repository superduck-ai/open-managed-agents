package db

import (
	"context"
	"time"
)

type BrainWorkerReport struct {
	WorkerID          string
	MaxSessions       int
	BusySessions      int
	ResidentSessions  int
	AdmissionFailures int
}

type BrainWorkerCircuit struct {
	Failures  int
	OpenUntil *time.Time
}

type BrainWorkerState struct {
	Circuit  BrainWorkerCircuit
	Inserted bool
}

type BrainWorkerLoad struct {
	MaxSessions      int
	BusySessions     int
	ResidentSessions int
}

func (d *DB) RecordBrainWorker(ctx context.Context, report BrainWorkerReport) (BrainWorkerState, error) {
	row, err := NewBrainWorkerMapper(d.mapperDB).Record(ctx, recordBrainWorkerParams{
		ExternalID:        report.WorkerID,
		MaxSessions:       report.MaxSessions,
		BusySessions:      report.BusySessions,
		ResidentSessions:  report.ResidentSessions,
		AdmissionFailures: report.AdmissionFailures,
	})
	if err != nil {
		return BrainWorkerState{}, err
	}
	return BrainWorkerState{
		Circuit:  BrainWorkerCircuit{Failures: row.CircuitFailures, OpenUntil: row.CircuitOpenUntil},
		Inserted: row.Inserted,
	}, nil
}

func (d *DB) UpdateBrainWorkerCircuit(ctx context.Context, workerID string, circuit BrainWorkerCircuit) error {
	rows, err := NewBrainWorkerMapper(d.mapperDB).UpdateCircuit(ctx, updateBrainWorkerCircuitParams{
		ExternalID:       workerID,
		CircuitFailures:  circuit.Failures,
		CircuitOpenUntil: circuit.OpenUntil,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListAvailableBrainWorkerLoads(ctx context.Context, seenAfter time.Time) ([]BrainWorkerLoad, error) {
	rows, err := NewBrainWorkerMapper(d.mapperDB).ListAvailableLoads(ctx, seenAfter)
	if err != nil {
		return nil, err
	}
	loads := make([]BrainWorkerLoad, len(rows))
	for index, row := range rows {
		loads[index] = BrainWorkerLoad(row)
	}
	return loads, nil
}

func (d *DB) DeleteStaleBrainWorkers(ctx context.Context, seenBefore time.Time) (int64, error) {
	return NewBrainWorkerMapper(d.mapperDB).DeleteStale(ctx, seenBefore)
}
