package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const (
	defaultBatchSize   = 10
	defaultMaxAttempts = 10
)

// Worker owns the object cleanup loop and its stable dependencies.
type Worker struct {
	database *db.DB
	client   storage.Client
	interval time.Duration
	logger   *slog.Logger
}

// NewWorker constructs an object cleanup worker.
func NewWorker(database *db.DB, client storage.Client, interval time.Duration, logger *slog.Logger) *Worker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Worker{
		database: database,
		client:   client,
		interval: interval,
		logger:   logging.LoggerOrDefault(logger),
	}
}

// Start launches the object cleanup loop.
func (w *Worker) Start(ctx context.Context) {
	if w == nil || w.database == nil || w.client == nil {
		return
	}
	workerID := fmt.Sprintf("object-cleanup-%d", os.Getpid())
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			if err := w.RunOnce(ctx, workerID); err != nil {
				w.logger.ErrorContext(ctx, "object cleanup worker", "error", err)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// RunOnce leases and processes one batch of object cleanup jobs.
func (w *Worker) RunOnce(ctx context.Context, workerID string) error {
	jobs, err := w.database.LeaseObjectCleanupJobs(ctx, workerID, defaultBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		store, storeErr := w.client.ForBucket(job.Bucket)
		if storeErr != nil {
			if err := w.database.FailObjectCleanupJob(ctx, job.UUID, job.Attempts, storeErr.Error(), time.Hour, defaultMaxAttempts); err != nil {
				errs = append(errs, fmt.Errorf("mark cleanup job %s failed: %w", job.ExternalID, err))
			}
			continue
		}

		if err := store.Delete(ctx, job.Key, storage.DeleteOptions{}); err != nil {
			delay := retryDelay(job.Attempts + 1)
			if markErr := w.database.FailObjectCleanupJob(ctx, job.UUID, job.Attempts, err.Error(), delay, defaultMaxAttempts); markErr != nil {
				errs = append(errs, fmt.Errorf("mark cleanup job %s retry: %w", job.ExternalID, markErr))
			}
			continue
		}
		if err := w.database.CompleteObjectCleanupJob(ctx, job.UUID); err != nil {
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
