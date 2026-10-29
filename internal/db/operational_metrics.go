package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	liveSessionStates            = []string{"running", "rescheduling"}
	liveEnvironmentWorkStates    = []string{"queued", "starting", "active", "stopping"}
	liveEnvironmentSandboxStates = []string{"creating", "running", "stopping"}
	requestedBrainClaimStates    = []string{"waiting", "claimed"}
	takeoverBrainClaimState      = "takeover"
	codeSessionWorkerStates      = []string{"idle", "running", "requires_action"}
	codeSessionConnectionStates  = []string{"connected", "disconnected"}
)

type CodeSessionState struct {
	WorkerStatus     string
	ConnectionStatus string
}

type OperationalStateCounts struct {
	Sessions                       map[string]int64
	StaleRunningSessions           int64
	CodeSessions                   map[CodeSessionState]int64
	EnvironmentWork                map[string]int64
	OldestQueuedWorkSeconds        int64
	StuckStartingWork              int64
	EnvironmentSandboxes           map[string]int64
	BrainClaims                    map[string]int64
	OldestWaitingBrainClaimSeconds int64
}

func (d *DB) PoolStat() *pgxpool.Stat {
	return d.pool.Stat()
}

func (d *DB) OperationalStateCounts(ctx context.Context, staleRunningBefore, stuckStartingBefore time.Time) (OperationalStateCounts, error) {
	mapper := NewOperationalMetricsMapper(d.mapperDB)
	var counts OperationalStateCounts
	sessions, err := mapper.CountLiveSessionsByStatus(ctx)
	if err != nil {
		return counts, fmt.Errorf("count live sessions: %w", err)
	}
	counts.Sessions = stateCounts(sessions, liveSessionStates)
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
	counts.EnvironmentWork = stateCounts(work, liveEnvironmentWorkStates)
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
	counts.EnvironmentSandboxes = stateCounts(sandboxes, liveEnvironmentSandboxStates)
	claims, err := mapper.CountRequestedBrainClaimsByState(ctx)
	if err != nil {
		return counts, fmt.Errorf("count requested brain claims: %w", err)
	}
	counts.BrainClaims = stateCounts(claims, requestedBrainClaimStates)
	if counts.BrainClaims[takeoverBrainClaimState], err = mapper.CountTakeoverPendingBrainClaims(ctx); err != nil {
		return counts, fmt.Errorf("count takeover pending brain claims: %w", err)
	}
	if counts.OldestWaitingBrainClaimSeconds, err = mapper.OldestWaitingBrainClaimAgeSeconds(ctx); err != nil {
		return counts, fmt.Errorf("read oldest waiting brain claim: %w", err)
	}
	return counts, nil
}

func stateCounts(rows []operationalStateCountRow, states []string) map[string]int64 {
	counts := make(map[string]int64, len(states))
	for _, state := range states {
		counts[state] = 0
	}
	for _, row := range rows {
		counts[row.State] = row.Total
	}
	return counts
}

func codeSessionStateCounts(rows []operationalCodeSessionCountRow) map[CodeSessionState]int64 {
	counts := make(map[CodeSessionState]int64, len(codeSessionWorkerStates)*len(codeSessionConnectionStates))
	for _, worker := range codeSessionWorkerStates {
		for _, connection := range codeSessionConnectionStates {
			counts[CodeSessionState{WorkerStatus: worker, ConnectionStatus: connection}] = 0
		}
	}
	for _, row := range rows {
		counts[CodeSessionState{WorkerStatus: row.WorkerStatus, ConnectionStatus: row.ConnectionStatus}] = row.Total
	}
	return counts
}
