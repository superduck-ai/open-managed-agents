package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const (
	defaultBatchSize   = 10
	defaultMaxAttempts = 10
)

func StartObjectCleanupWorker(ctx context.Context, database *db.DB, store storage.ObjectStore, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	workerID := fmt.Sprintf("object-cleanup-%d", os.Getpid())
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			if err := RunObjectCleanupOnce(ctx, database, store, workerID); err != nil {
				log.Printf("object cleanup worker: %v", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func RunObjectCleanupOnce(ctx context.Context, database *db.DB, store storage.ObjectStore, workerID string) error {
	jobs, err := database.LeaseObjectCleanupJobs(ctx, workerID, defaultBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if job.Bucket != "" && job.Bucket != store.Bucket() {
			if err := database.FailObjectCleanupJob(ctx, job.ID, job.Attempts, "cleanup bucket does not match configured object store", time.Hour, defaultMaxAttempts); err != nil {
				errs = append(errs, fmt.Errorf("mark cleanup job %s failed: %w", job.ExternalID, err))
			}
			continue
		}

		if err := store.Delete(ctx, job.Key); err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := database.FailObjectCleanupJob(ctx, job.ID, job.Attempts, err.Error(), delay, defaultMaxAttempts); markErr != nil {
				errs = append(errs, fmt.Errorf("mark cleanup job %s retry: %w", job.ExternalID, markErr))
			}
			continue
		}
		if err := database.CompleteObjectCleanupJob(ctx, job.ID); err != nil {
			errs = append(errs, fmt.Errorf("complete cleanup job %s: %w", job.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

func retryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(attempts*attempts) * time.Minute
}
