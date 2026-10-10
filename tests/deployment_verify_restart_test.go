package tests

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
)

func TestVerifyDeploymentRestart(t *testing.T) {
	f := newDeploymentVerification(t)
	created := f.create(t)
	release := holdDeploymentRun(t, f.app, created.ID)
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Second)
	job := f.insert(t, created.ID, at, 4)
	process := f.process(t, "before_commit")
	deploymentWait(t, "transaction blocked before Run insert", 8*time.Second, func() bool {
		var blocked bool
		if err := f.app.pool.QueryRow(t.Context(), `select exists(select 1 from pg_stat_activity where datname=current_database() and wait_event='advisory' and query ilike '%insert into deployment_runs%')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		return blocked
	})
	if f.job(t, job.ID).State != rivertype.JobStateRunning {
		t.Fatal("fault did not hold a running job")
	}
	process.stop(t, true)
	release()
	f.runs(t, created.ID, 0)
	f.effects(t, created.ID, 0)
	f.proof(t, "uncommitted_crash_rolled_back")
	process = f.process(t, "recover")
	done := f.waitJob(t, job.ID, rivertype.JobStateCompleted)
	assertDeploymentRescued(t, done)
	runs := f.success(t, created.ID, 1)
	if runs[0].ScheduledAt == nil || !runs[0].ScheduledAt.Equal(at) {
		t.Fatal("crash recovery changed occurrence")
	}
	process.stop(t, false)
	f.proof(t, "uncommitted_restart_recovered")

	committed := f.create(t)
	job = f.insert(t, committed.ID, time.Now().Add(-time.Second), 4)
	process = f.process(t, "after_commit")
	deploymentWait(t, "business commit before River finalization", 8*time.Second, func() bool { _, err := os.Stat(process.committed); return err == nil })
	if f.job(t, job.ID).State != rivertype.JobStateRunning {
		t.Fatal("River finalized before commit barrier")
	}
	before := f.success(t, committed.ID, 1)[0]
	process.stop(t, true)
	f.success(t, committed.ID, 1)
	f.proof(t, "committed_crash_preserved")
	process = f.process(t, "recover")
	done = f.waitJob(t, job.ID, rivertype.JobStateCompleted)
	assertDeploymentRescued(t, done)
	after := f.success(t, committed.ID, 1)[0]
	if before.ExternalID != after.ExternalID || *before.SessionExternalID != *after.SessionExternalID {
		t.Fatal("recovered job replaced committed Run or Session")
	}
	process.stop(t, false)
	f.proof(t, "committed_restart_deduplicated")

	overdue := f.create(t)
	due := f.due(t, overdue.ID)
	beforeSchedule, err := f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), overdue.ID)
	if err != nil || !beforeSchedule.NextRunAt.Equal(due) {
		t.Fatal("offline due schedule not persisted")
	}
	process = f.process(t, "recover")
	deploymentWait(t, "offline durable schedule recovered", 30*time.Second, func() bool { return len(listDeploymentRuns(t, f.app, "deployment_id="+overdue.ID).Data) == 1 })
	f.waitJob(t, f.scheduledJob(t, overdue.ID, due), rivertype.JobStateCompleted)
	f.success(t, overdue.ID, 1)
	afterSchedule, err := f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), overdue.ID)
	if err != nil || !afterSchedule.NextRunAt.After(time.Now()) {
		t.Fatal("restart did not advance overdue cursor")
	}
	process.stop(t, false)
	f.proof(t, "overdue_schedule_recovered")
}

func assertDeploymentRescued(t *testing.T, job *rivertype.JobRow) {
	t.Helper()
	if job.Attempt < 2 {
		t.Fatal("recovery did not re-execute job")
	}
	for _, failure := range job.Errors {
		if strings.Contains(failure.Error, "Stuck job rescued by JobRescuer") {
			return
		}
	}
	t.Fatal("recovery did not use the actual River rescuer")
}
