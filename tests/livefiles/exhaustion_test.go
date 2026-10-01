package livefiles

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestFilesExhaustion(t *testing.T) {
	e := newFilesEnv(t)
	jobID := "job_" + uuid.NewString()
	key := "exhaustion/" + uuid.NewString()
	content := []byte("retain until explicit recovery")
	_, err := e.objects.Upload(e.ctx, key, bytes.NewReader(content), storage.UploadOptions{Size: int64(len(content))})
	requireOK(t, err)
	e.deny(t, "DELETE", key, true)
	requireOK(t, e.database.EnqueueScheduledObjectCleanupResourceJob(e.ctx, jobID, e.key.WorkspaceUUID.String(), e.objects.Name(), key, "file", "file_exhaustion", time.Now().Add(time.Hour)))
	_, err = e.database.GetObjectCleanupState(e.ctx, uuid.NewString(), jobID)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatal("cleanup state leaked across workspace")
	}
	client, err := storage.New(e.cfg.Storage)
	requireOK(t, err)
	worker := cleanup.NewWorker(e.database, client, time.Second, nil)
	for attempt := 1; attempt <= 10; attempt++ {
		requireOK(t, e.database.ExpediteObjectCleanupJob(e.ctx, jobID))
		started := time.Now()
		requireOK(t, worker.RunOnce(e.ctx, "verify-exhaustion"))
		e.await(t, "cleanup attempt persisted", func() bool {
			state, err := e.database.GetObjectCleanupState(e.ctx, e.key.WorkspaceUUID.String(), jobID)
			requireOK(t, err)
			return state.Attempts == attempt
		})
		state, err := e.database.GetObjectCleanupState(e.ctx, e.key.WorkspaceUUID.String(), jobID)
		requireOK(t, err)
		want := "retry"
		if attempt == 10 {
			want = "failed"
		}
		delay := time.Duration(min(attempt, 6)*min(attempt, 6)) * time.Minute
		if state.Status != want || state.RunAfter.Before(started.Add(delay)) || state.RunAfter.After(time.Now().Add(delay)) || len(e.deleteAttempts(t, key)) != attempt {
			t.Fatalf("cleanup attempt %d: state=%+v", attempt, state)
		}
	}
	e.proof(t, "cleanup_retry_budget_exhausted")
	e.deny(t, "DELETE", key, false)
	for range 3 {
		requireOK(t, worker.RunOnce(e.ctx, "verify-terminal"))
	}
	state, err := e.database.GetObjectCleanupState(e.ctx, e.key.WorkspaceUUID.String(), jobID)
	requireOK(t, err)
	if state.Status != "failed" || state.Attempts != 10 || len(e.deleteAttempts(t, key)) != 10 {
		t.Fatal("failed cleanup resumed without explicit recovery")
	}
	e.assertObject(t, key, content)
	e.proof(t, "failed_job_stays_terminal")
	requireOK(t, e.database.ExpediteObjectCleanupJob(e.ctx, jobID))
	requireOK(t, worker.RunOnce(e.ctx, "verify-recovery"))
	e.await(t, "explicit cleanup recovery", func() bool {
		state, err := e.database.GetObjectCleanupState(e.ctx, e.key.WorkspaceUUID.String(), jobID)
		requireOK(t, err)
		return state.Status == "completed"
	})
	if !e.objectAbsent(t, key) || len(e.deleteAttempts(t, key)) != 11 {
		t.Fatal("explicit recovery did not delete object exactly once")
	}
	requireOK(t, e.database.ExpediteObjectCleanupJob(e.ctx, jobID))
	requireOK(t, worker.RunOnce(e.ctx, "verify-completed"))
	if len(e.deleteAttempts(t, key)) != 11 {
		t.Fatal("completed cleanup revived")
	}
	e.proof(t, "explicit_recovery_completed")
}
