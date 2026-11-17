package evaluations

import (
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
)

func TestSettleNeedsInsertSkipsLiveWork(t *testing.T) {
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	for _, job := range []*rivertype.JobRow{
		{State: rivertype.JobStateAvailable},
		{State: rivertype.JobStateRetryable},
		{State: rivertype.JobStateRunning, AttemptedAt: &recent},
		{State: rivertype.JobStateRunning},
	} {
		insert, stale := settleNeedsInsert([]*rivertype.JobRow{job}, now)
		if insert || stale {
			t.Fatalf("state %s insert=%v stale=%v", job.State, insert, stale)
		}
	}
}

func TestSettleNeedsInsertReplacesStaleRunningJob(t *testing.T) {
	now := time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC)
	started := now.Add(-settleJobTimeout)
	insert, stale := settleNeedsInsert([]*rivertype.JobRow{{
		State: rivertype.JobStateRunning, AttemptedAt: &started,
	}}, now)
	if !insert || !stale {
		t.Fatalf("insert=%v stale=%v", insert, stale)
	}
	insert, stale = settleNeedsInsert(nil, now)
	if !insert || stale {
		t.Fatalf("empty insert=%v stale=%v", insert, stale)
	}
}
