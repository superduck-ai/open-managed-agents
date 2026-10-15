package db

import (
	"context"
	"fmt"
	"time"
)

type StateCount struct {
	State string
	Total int64
}

type CodeSessionStateCount struct {
	WorkerStatus     string
	ConnectionStatus string
	Total            int64
}

type OperationalStateCounts struct {
	Sessions                []StateCount
	StaleRunningSessions    int64
	CodeSessions            []CodeSessionStateCount
	EnvironmentWork         []StateCount
	OldestQueuedWorkSeconds int64
	StuckStartingWork       int64
	EnvironmentSandboxes    []StateCount
}

func (d *DB) OperationalStateCounts(ctx context.Context, staleRunningBefore, stuckStartingBefore time.Time) (OperationalStateCounts, error) {
	mapper := NewOperationalMetricsMapper(d.mapperDB)
	var counts OperationalStateCounts
	sessions, err := mapper.CountLiveSessionsByStatus(ctx)
	if err != nil {
		return counts, fmt.Errorf("count live sessions: %w", err)
	}
	counts.Sessions = stateCounts(sessions)
	if counts.StaleRunningSessions, err = mapper.CountStaleRunningSessions(ctx, staleRunningBefore); err != nil {
		return counts, fmt.Errorf("count stale running sessions: %w", err)
	}
	codeSessions, err := mapper.CountActiveCodeSessionsByWorkerState(ctx)
	if err != nil {
		return counts, fmt.Errorf("count active code sessions: %w", err)
	}
	counts.CodeSessions = codeSessionStateCounts(codeSessions)
	work, err := mapper.CountLiveEnvironmentWorkByState(ctx)
	if err != nil {
		return counts, fmt.Errorf("count live environment work: %w", err)
	}
	counts.EnvironmentWork = stateCounts(work)
	if counts.OldestQueuedWorkSeconds, err = mapper.OldestQueuedEnvironmentWorkAgeSeconds(ctx); err != nil {
		return counts, fmt.Errorf("read oldest queued environment work: %w", err)
	}
	if counts.StuckStartingWork, err = mapper.CountStuckStartingEnvironmentWork(ctx, stuckStartingBefore); err != nil {
		return counts, fmt.Errorf("count stuck starting environment work: %w", err)
	}
	sandboxes, err := mapper.CountLiveEnvironmentSandboxesByState(ctx)
	if err != nil {
		return counts, fmt.Errorf("count live environment sandboxes: %w", err)
	}
	counts.EnvironmentSandboxes = stateCounts(sandboxes)
	return counts, nil
}

func stateCounts(rows []operationalStateCountRow) []StateCount {
	counts := make([]StateCount, len(rows))
	for index, row := range rows {
		counts[index] = StateCount{State: row.State, Total: row.Total}
	}
	return counts
}

func codeSessionStateCounts(rows []operationalCodeSessionCountRow) []CodeSessionStateCount {
	counts := make([]CodeSessionStateCount, len(rows))
	for index, row := range rows {
		counts[index] = CodeSessionStateCount{WorkerStatus: row.WorkerStatus, ConnectionStatus: row.ConnectionStatus, Total: row.Total}
	}
	return counts
}
