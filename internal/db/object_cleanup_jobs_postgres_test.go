package db

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestObjectCleanupJobsPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	workspaceUUID := "52000000-0000-0000-0000-000000000002"

	t.Run("missing cleanup job", func(t *testing.T) {
		if err := store.ExpediteObjectCleanupJob(ctx, "job_missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expedite missing job = %v, want ErrNotFound", err)
		}
	})

	t.Run("scheduled payload cleanup preserves lifecycle", func(t *testing.T) {
		if err := store.EnqueueScheduledObjectCleanupResourceJob(ctx, "job_payload", workspaceUUID,
			"payloads", "tenant/payload.json", "code_session", "cs_test", time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		jobs, err := store.LeaseObjectCleanupJobs(ctx, "worker", 10)
		if err != nil || len(jobs) != 0 {
			t.Fatalf("future jobs = %+v, %v", jobs, err)
		}
		if err := store.ExpediteObjectCleanupJob(ctx, "job_payload"); err != nil {
			t.Fatal(err)
		}
		jobs, err = store.LeaseObjectCleanupJobs(ctx, "worker", 10)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("expedited jobs = %+v, %v", jobs, err)
		}
		job := jobs[0]
		if job.WorkspaceUUID != workspaceUUID || job.Bucket != "payloads" || job.Key != "tenant/payload.json" || job.FileExternalID != "" {
			t.Fatalf("hydrated cleanup job = %+v", job)
		}
		if err := store.FailObjectCleanupJob(ctx, job.UUID, job.Attempts, "temporary", 0, 3); err != nil {
			t.Fatal(err)
		}
		jobs, err = store.LeaseObjectCleanupJobs(ctx, "worker", 10)
		if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
			t.Fatalf("retry jobs = %+v, %v", jobs, err)
		}
		if err := store.CompleteObjectCleanupJob(ctx, job.UUID); err != nil {
			t.Fatal(err)
		}
		if err := store.ExpediteObjectCleanupJob(ctx, job.ExternalID); err != nil {
			t.Fatal(err)
		}
		jobs, err = store.LeaseObjectCleanupJobs(ctx, "worker", 10)
		if err != nil || len(jobs) != 0 {
			t.Fatalf("completed job must not be revived: %+v, %v", jobs, err)
		}
	})

	t.Run("file cleanup keeps file identity", func(t *testing.T) {
		if err := store.EnqueueObjectCleanupJob(ctx, workspaceUUID, "files", "tenant/file.txt", "file_test"); err != nil {
			t.Fatal(err)
		}
		jobs, err := store.LeaseObjectCleanupJobs(ctx, "worker", 10)
		if err != nil || len(jobs) != 1 || jobs[0].FileExternalID != "file_test" {
			t.Fatalf("file cleanup jobs = %+v, %v", jobs, err)
		}
	})
}
