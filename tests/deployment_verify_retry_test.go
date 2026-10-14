package tests

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
)

func TestVerifyDeploymentRetry(t *testing.T) {
	f := newDeploymentVerification(t)
	created := f.create(t)
	remove := deploymentRunFault(t, f.app, created.ID, "raise exception 'verification transient database failure';")
	at := time.Now().UTC().Truncate(time.Second).Add(-time.Second)
	job := f.insert(t, created.ID, at, 3)
	_, stop := startDeploymentScheduler(t, f.app)
	defer stop()
	deploymentWait(t, "failed River attempt", 15*time.Second, func() bool { return len(f.job(t, job.ID).Errors) > 0 })
	failed := f.job(t, job.ID)
	if failed.State == rivertype.JobStateCompleted || failed.Attempt < 1 || !failed.ScheduledAt.After(at) {
		t.Fatal("database error was not scheduled for retry")
	}
	var stamped deploymentjobs.Args
	if err := json.Unmarshal(failed.EncodedArgs, &stamped); err != nil || !stamped.ScheduledAt.Equal(at) {
		t.Fatal("retry changed nominal occurrence")
	}
	f.runs(t, created.ID, 0)
	f.effects(t, created.ID, 0)
	f.proof(t, "failed_attempt_rolled_back")
	remove()
	done := f.waitJob(t, job.ID, rivertype.JobStateCompleted)
	if done.Attempt < 2 || len(done.Errors) < 1 {
		t.Fatal("no automatic retry observed")
	}
	runs := f.success(t, created.ID, 1)
	if runs[0].ScheduledAt == nil || !runs[0].ScheduledAt.Equal(at) {
		t.Fatal("successful retry used the retry time as occurrence")
	}
	if retrieveDeployment(t, f.app, created.ID).Status != "active" {
		t.Fatal("transient error paused deployment")
	}
	f.proof(t, "automatic_retry_matches")

	exhausted := f.create(t)
	removeExhausted := deploymentRunFault(t, f.app, exhausted.ID, "raise exception 'verification persistent database failure';")
	job = f.insert(t, exhausted.ID, time.Now().Add(-time.Second), 2)
	done = f.waitJob(t, job.ID, rivertype.JobStateDiscarded)
	if done.Attempt != 2 || len(done.Errors) != 2 {
		t.Fatalf("incorrect exhausted attempts: %+v", done)
	}
	f.runs(t, exhausted.ID, 0)
	f.effects(t, exhausted.ID, 0)
	removeExhausted()
	if f.job(t, job.ID).State != rivertype.JobStateDiscarded {
		t.Fatal("discarded job was resurrected without retry")
	}
	f.proof(t, "exhausted_job_has_no_effects")

	invalid := f.create(t)
	archiveEnvironment(t, f.app, f.environmentID)
	job = f.insert(t, invalid.ID, time.Now().Add(-time.Second), 3)
	done = f.waitJob(t, job.ID, rivertype.JobStateCompleted)
	if done.Attempt != 1 || len(done.Errors) != 0 {
		t.Fatal("permanent reference failure was retried")
	}
	failure := f.runs(t, invalid.ID, 1)[0]
	if failure.SessionExternalID != nil || !json.Valid(failure.Error) {
		t.Fatal("failed run created a Session or omitted error")
	}
	var runError struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(failure.Error, &runError); err != nil || runError.Type != "environment_archived_error" {
		t.Fatalf("unexpected reference error: %s", failure.Error)
	}
	f.effects(t, invalid.ID, 0)
	paused := retrieveDeployment(t, f.app, invalid.ID)
	if paused.Status != "paused" {
		t.Fatal("permanent reference failure did not pause deployment")
	}
	if _, err := f.app.deploymentJobs.DurablePeriodicJobGet(t.Context(), invalid.ID); !errors.Is(err, rivertype.ErrNotFound) {
		t.Fatalf("failure retained schedule: %v", err)
	}
	f.proof(t, "reference_failure_paused")
}
