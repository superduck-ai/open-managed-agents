package codesessions

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	brainClaimMaxTasks             = 32
	brainClaimMaxDelay             = 500 * time.Millisecond
	brainIdleResidentWeight        = 0.5
	brainWorkerLiveWindow          = brainClaimMaxBlock + 15*time.Second
	brainWorkerStaleAfter          = time.Hour
	brainCircuitFailureThreshold   = 3
	brainCircuitBaseHold           = 15 * time.Second
	brainCircuitMaxHold            = 5 * time.Minute
	brainCircuitHalfOpenClaimLimit = 1
)

type BrainClaimRequest struct {
	WorkerID          string
	Block             time.Duration
	MaxTasks          int
	Load              db.BrainWorkerLoad
	AdmissionFailures int
}

type brainClaimPlan struct {
	maxTasks  int
	openUntil time.Time
	delay     time.Duration
}

func (p brainClaimPlan) claimable(now time.Time) bool {
	return !now.Before(p.openUntil)
}

func brainWorkerOccupancy(load db.BrainWorkerLoad) float64 {
	if load.MaxSessions <= 0 {
		return 1
	}
	idle := max(load.ResidentSessions-load.BusySessions, 0)
	occupancy := (float64(load.BusySessions) + brainIdleResidentWeight*float64(idle)) / float64(load.MaxSessions)
	return min(max(occupancy, 0), 1)
}

func brainClaimDelay(own db.BrainWorkerLoad, fleet []db.BrainWorkerLoad) time.Duration {
	occupancy := brainWorkerOccupancy(own)
	lowest := occupancy
	for _, load := range fleet {
		lowest = min(lowest, brainWorkerOccupancy(load))
	}
	return time.Duration((occupancy - lowest) * float64(brainClaimMaxDelay))
}

func brainCircuitHold(failures int) time.Duration {
	hold := brainCircuitBaseHold
	for range failures - brainCircuitFailureThreshold {
		hold *= 2
		if hold >= brainCircuitMaxHold {
			return brainCircuitMaxHold
		}
	}
	return hold
}

func nextBrainWorkerCircuit(current db.BrainWorkerCircuit, failures int, now time.Time) (db.BrainWorkerCircuit, bool) {
	if failures < brainCircuitFailureThreshold {
		closed := db.BrainWorkerCircuit{}
		return closed, current.Failures != 0 || current.OpenUntil != nil
	}
	if failures <= current.Failures {
		return current, false
	}
	openUntil := now.Add(brainCircuitHold(failures))
	return db.BrainWorkerCircuit{Failures: failures, OpenUntil: &openUntil}, true
}

func newBrainClaimPlan(request BrainClaimRequest, circuit db.BrainWorkerCircuit, fleet []db.BrainWorkerLoad) brainClaimPlan {
	plan := brainClaimPlan{maxTasks: request.MaxTasks, delay: brainClaimDelay(request.Load, fleet)}
	if circuit.Failures >= brainCircuitFailureThreshold {
		plan.maxTasks = min(plan.maxTasks, brainCircuitHalfOpenClaimLimit)
		if circuit.OpenUntil != nil {
			plan.openUntil = *circuit.OpenUntil
		}
	}
	return plan
}

func (s *Service) planBrainClaims(ctx context.Context, request BrainClaimRequest) (brainClaimPlan, error) {
	state, err := s.db.RecordBrainWorker(ctx, db.BrainWorkerReport{
		WorkerID:          request.WorkerID,
		MaxSessions:       request.Load.MaxSessions,
		BusySessions:      request.Load.BusySessions,
		ResidentSessions:  request.Load.ResidentSessions,
		AdmissionFailures: request.AdmissionFailures,
	})
	if err != nil {
		return brainClaimPlan{}, err
	}
	now := time.Now().UTC()
	if state.Inserted {
		s.deleteStaleBrainWorkers(ctx, now)
	}
	circuit, changed := nextBrainWorkerCircuit(state.Circuit, request.AdmissionFailures, now)
	if changed {
		if err := s.db.UpdateBrainWorkerCircuit(ctx, request.WorkerID, circuit); err != nil {
			return brainClaimPlan{}, err
		}
		s.logBrainWorkerCircuit(ctx, request.WorkerID, circuit)
	}
	fleet, err := s.db.ListAvailableBrainWorkerLoads(ctx, now.Add(-brainWorkerLiveWindow))
	if err != nil {
		return brainClaimPlan{}, err
	}
	return newBrainClaimPlan(request, circuit, fleet), nil
}

func (s *Service) deleteStaleBrainWorkers(ctx context.Context, now time.Time) {
	deleted, err := s.db.DeleteStaleBrainWorkers(ctx, now.Add(-brainWorkerStaleAfter))
	if err != nil {
		s.logger.WarnContext(ctx, "delete stale brain workers", "error", err)
		return
	}
	if deleted > 0 {
		s.logger.InfoContext(ctx, "deleted stale brain workers", "count", deleted)
	}
}

func (s *Service) logBrainWorkerCircuit(ctx context.Context, workerID string, circuit db.BrainWorkerCircuit) {
	if circuit.OpenUntil == nil {
		s.logger.InfoContext(ctx, "brain worker circuit closed", "worker_id", workerID)
		return
	}
	s.logger.WarnContext(ctx, "brain worker circuit opened", "worker_id", workerID,
		"admission_failures", circuit.Failures, "open_until", circuit.OpenUntil.Format(time.RFC3339))
}

func waitBrainClaimDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
