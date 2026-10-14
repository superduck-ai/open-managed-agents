package tests

import (
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
)

func TestVerifyDeploymentLifecycle(t *testing.T) {
	f := newDeploymentVerification(t)
	archived := f.create(t)
	archiveDeployment(t, f.app, archived.ID)
	deploymentRequestRejected(t, f.app, archived.ID)
	f.runs(t, archived.ID, 0)
	f.effects(t, archived.ID, 0)
	f.proof(t, "invalid_runs_leave_no_effects")

	created := f.create(t)
	manual := runDeployment(t, f.app, created.ID)
	runs := f.success(t, created.ID, 1)
	if runs[0].ExternalID != manual.ID || runs[0].TriggerType != "manual" || runs[0].ScheduledAt != nil {
		t.Fatal("manual run trigger changed")
	}
	f.proof(t, "manual_run_effects_match")

	due := f.due(t, created.ID)
	_, stop := startDeploymentScheduler(t, f.app)
	defer stop()
	deploymentWait(t, "durable schedule execution", 20*time.Second, func() bool {
		page := listDeploymentRuns(t, f.app, "deployment_id="+created.ID)
		return len(page.Data) == 2
	})
	f.success(t, created.ID, 2)
	job := f.scheduledJob(t, created.ID, due)
	f.waitJob(t, job, rivertype.JobStateCompleted)
	after, err := f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), created.ID)
	if err != nil || !after.NextRunAt.After(time.Now()) {
		t.Fatalf("durable cursor did not advance: %+v %v", after, err)
	}
	f.proof(t, "durable_schedule_executed")

	paused := pauseDeployment(t, f.app, created.ID)
	if paused.Status != "paused" {
		t.Fatal("pause not persisted")
	}
	if _, err := f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), created.ID); !errors.Is(err, rivertype.ErrNotFound) {
		t.Fatalf("pause retained schedule: %v", err)
	}
	queued := f.insert(t, created.ID, time.Now().Add(-time.Second), 3)
	f.waitJob(t, queued.ID, rivertype.JobStateCompleted)
	f.success(t, created.ID, 2)
	runDeployment(t, f.app, created.ID)
	f.success(t, created.ID, 3)
	if retrieveDeployment(t, f.app, created.ID).Status != "paused" {
		t.Fatal("manual run unexpectedly resumed deployment")
	}
	unpauseDeployment(t, f.app, created.ID)
	before, err := f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	unpauseDeployment(t, f.app, created.ID)
	after, err = f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), created.ID)
	if err != nil || !before.NextRunAt.Equal(after.NextRunAt) {
		t.Fatal("repeated unpause reset cursor")
	}
	archiveDeployment(t, f.app, created.ID)
	if _, err := f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), created.ID); !errors.Is(err, rivertype.ErrNotFound) {
		t.Fatalf("archive retained schedule: %v", err)
	}
	queued = f.insert(t, created.ID, time.Now().Add(-time.Second), 3)
	f.waitJob(t, queued.ID, rivertype.JobStateCompleted)
	f.success(t, created.ID, 3)
	deploymentRequestRejected(t, f.app, created.ID)
	f.proof(t, "pause_archive_stop_schedule")
}

func (f *deploymentVerification) due(t *testing.T, id string) time.Time {
	t.Helper()
	deployment, err := f.app.db.GetDeployment(t.Context(), f.workspaceUUID, id)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := deploymentjobs.UpsertOpts(f.workspaceUUID, id, deployment.Schedule)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Second)
	opts.Schedule.NextRunAt = at
	if _, err := f.app.deploymentJobs.DurablePeriodicJobUpsert(t.Context(), opts); err != nil {
		t.Fatal(err)
	}
	return at
}

func (f *deploymentVerification) scheduledJob(t *testing.T, id string, at time.Time) int64 {
	t.Helper()
	var jobID int64
	if err := f.app.pool.QueryRow(t.Context(), `select id from public.river_job where kind=$1 and args->>'deployment_id'=$2 and (args->>'scheduled_at')::timestamptz=$3`, deploymentjobs.Args{}.Kind(), id, at).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	return jobID
}
