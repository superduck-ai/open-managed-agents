package tests

import (
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
)

func TestVerifyDeploymentIdempotency(t *testing.T) {
	f := newDeploymentVerification(t)
	created := f.create(t)
	stale := f.insertSchedule(t, created.ID, time.Now().Add(-time.Second), 3, deploymentjobs.Schedule{Type: "cron", Expression: "*/10 * * * *", Timezone: "UTC"})
	_, stop := startDeploymentScheduler(t, f.app)
	defer stop()
	f.waitJob(t, stale.ID, rivertype.JobStateCompleted)
	f.runs(t, created.ID, 0)
	f.effects(t, created.ID, 0)
	f.proof(t, "stale_jobs_have_no_effects")

	release := holdDeploymentRun(t, f.app, created.ID)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Second)
	jobs := make([]int64, 8)
	for i := range jobs {
		jobs[i] = f.insert(t, created.ID, at, 3).ID
	}
	deploymentWait(t, "eight concurrently running duplicate jobs", 8*time.Second, func() bool {
		for _, id := range jobs {
			if f.job(t, id).State != rivertype.JobStateRunning {
				return false
			}
		}
		return true
	})
	release()
	for _, id := range jobs {
		job := f.waitJob(t, id, rivertype.JobStateCompleted)
		if job.Attempt != 1 || len(job.Errors) != 0 {
			t.Fatal("duplicate occurrence should complete as a no-op")
		}
	}
	runs := f.success(t, created.ID, 1)
	if runs[0].ScheduledAt == nil || !runs[0].ScheduledAt.Equal(at) {
		t.Fatal("duplicate occurrence lost nominal timestamp")
	}
	original := *runs[0].SessionExternalID
	f.proof(t, "duplicate_occurrence_single_effect")

	second := at.Add(-time.Minute)
	job := f.insert(t, created.ID, second, 3)
	f.waitJob(t, job.ID, rivertype.JobStateCompleted)
	runs = f.success(t, created.ID, 2)
	if runs[0].SessionExternalID == nil || runs[1].SessionExternalID == nil || *runs[0].SessionExternalID == *runs[1].SessionExternalID {
		t.Fatal("distinct occurrences share one Session")
	}
	if *runs[0].SessionExternalID != original && *runs[1].SessionExternalID != original {
		t.Fatal("later occurrence replaced original Session")
	}
	f.proof(t, "distinct_occurrence_preserved")
}
