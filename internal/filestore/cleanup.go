package filestore

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

	"github.com/google/uuid"
)

const (
	filestoreCleanupBatchSize           = 10
	filestoreFilesystemCleanupBatchSize = 100
	filestoreCleanupMaxAttempts         = 10
	filestoreTTLSweepBatchSize          = 100
	filestoreCleanupPollInterval        = 30 * time.Second
	filestoreTTLSweepInterval           = 5 * time.Minute
)

type filestoreCleanupDatabase interface {
	LeaseFilestoreFilesystemCleanupJobs(context.Context, string, int, int) ([]db.FilestoreFilesystemCleanupJob, error)
	ProcessLeasedFilestoreFilesystemCleanupJob(context.Context, int64, string, int) (bool, error)
	FailLeasedFilestoreFilesystemCleanupJob(context.Context, int64, string, string, time.Duration, int) error
	LeaseFilestoreObjectCleanupJobs(context.Context, string, int, int) ([]db.FilestoreObjectCleanupJob, error)
	CompleteLeasedFilestoreObjectCleanupJob(context.Context, int64, string) error
	FailLeasedFilestoreObjectCleanupJob(context.Context, int64, string, string, time.Duration, int) error
	ExpireSessionNamespaceNodes(context.Context, int) ([]db.FilestoreObjectCleanupJob, error)
}

// CleanupWorker owns the filestore cleanup and TTL sweep loops.
type CleanupWorker struct {
	database filestoreCleanupDatabase
	client   storage.Client
	logger   *slog.Logger
}

// NewCleanupWorker constructs a filestore cleanup worker.
func NewCleanupWorker(database filestoreCleanupDatabase, client storage.Client, logger *slog.Logger) *CleanupWorker {
	return &CleanupWorker{
		database: database,
		client:   client,
		logger:   logging.LoggerOrDefault(logger),
	}
}

// Start 启动单个后台循环，分别处理对象清理任务与 TTL 到期扫描。
// 两类工作都会在启动时立即执行一次，不必等待首个定时周期。
func (w *CleanupWorker) Start(ctx context.Context) {
	if w == nil || w.database == nil || w.client == nil {
		return
	}
	workerID := fmt.Sprintf("filestore-cleanup-%d-%s", os.Getpid(), uuid.NewString())
	go w.runLoop(ctx, workerID)
}

func (w *CleanupWorker) runLoop(ctx context.Context, workerID string) {
	cleanupTicker := time.NewTicker(filestoreCleanupPollInterval)
	defer cleanupTicker.Stop()
	ttlTicker := time.NewTicker(filestoreTTLSweepInterval)
	defer ttlTicker.Stop()

	if ctx.Err() != nil {
		return
	}
	w.runFilesystemCleanupAndLog(ctx, workerID)
	if ctx.Err() != nil {
		return
	}
	w.runTTLSweepAndLog(ctx)
	if ctx.Err() != nil {
		return
	}
	w.runCleanupAndLog(ctx, workerID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-cleanupTicker.C:
			w.runFilesystemCleanupAndLog(ctx, workerID)
			w.runCleanupAndLog(ctx, workerID)
		case <-ttlTicker.C:
			w.runTTLSweepAndLog(ctx)
		}
	}
}

func (w *CleanupWorker) runFilesystemCleanupAndLog(ctx context.Context, workerID string) {
	if err := w.RunFilesystemCleanupOnce(ctx, workerID); err != nil {
		w.logger.ErrorContext(ctx, "filestore filesystem cleanup worker", "error", err)
	}
}

func (w *CleanupWorker) runCleanupAndLog(ctx context.Context, workerID string) {
	if err := w.RunCleanupOnce(ctx, workerID); err != nil {
		w.logger.ErrorContext(ctx, "filestore cleanup worker", "error", err)
	}
}

func (w *CleanupWorker) runTTLSweepAndLog(ctx context.Context) {
	if err := w.RunTTLSweepOnce(ctx); err != nil {
		w.logger.ErrorContext(ctx, "filestore TTL sweep", "error", err)
	}
}

// RunFilesystemCleanupOnce 把已删除 filesystem 的一批元数据转换成对象清理任务。
// 此阶段只访问数据库；真正的 S3 删除仍由对象任务在事务外完成。
func (w *CleanupWorker) RunFilesystemCleanupOnce(ctx context.Context, workerID string) error {
	jobs, err := w.database.LeaseFilestoreFilesystemCleanupJobs(
		ctx, workerID, filestoreCleanupBatchSize, filestoreCleanupMaxAttempts,
	)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		_, processErr := w.database.ProcessLeasedFilestoreFilesystemCleanupJob(
			ctx,
			job.ID,
			workerID,
			filestoreFilesystemCleanupBatchSize,
		)
		if processErr == nil {
			continue
		}
		if errors.Is(processErr, context.Canceled) || errors.Is(processErr, context.DeadlineExceeded) {
			errs = append(errs, processErr)
			break
		}
		if err := w.database.FailLeasedFilestoreFilesystemCleanupJob(
			ctx,
			job.ID,
			workerID,
			processErr.Error(),
			filestoreCleanupRetryDelay(job.Attempts+1),
			filestoreCleanupMaxAttempts,
		); err != nil {
			errs = append(errs, fmt.Errorf("mark filesystem cleanup job %s retry: %w", job.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

// RunCleanupOnce 租约并处理一批有界的对象清理任务。
// 每条任务按自身持久化的 bucket 选择对象存储；对象已不存在等同于目标已达成。
func (w *CleanupWorker) RunCleanupOnce(ctx context.Context, workerID string) error {
	jobs, err := w.database.LeaseFilestoreObjectCleanupJobs(
		ctx, workerID, filestoreCleanupBatchSize, filestoreCleanupMaxAttempts,
	)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		store, storeErr := w.client.ForBucket(job.Bucket)
		if storeErr != nil {
			if err := w.database.FailLeasedFilestoreObjectCleanupJob(
				ctx,
				job.ID,
				workerID,
				storeErr.Error(),
				time.Hour,
				filestoreCleanupMaxAttempts,
			); err != nil {
				errs = append(errs, fmt.Errorf("mark cleanup job %s failed: %w", job.ExternalID, err))
			}
			continue
		}

		deleteErr := store.Delete(ctx, job.Key, storage.DeleteOptions{
			VersionID:   job.VersionID,
			AllVersions: job.VersionID == "",
		})
		if errors.Is(deleteErr, context.Canceled) || errors.Is(deleteErr, context.DeadlineExceeded) {
			errs = append(errs, deleteErr)
			break
		}
		if deleteErr != nil && !errors.Is(deleteErr, storage.ErrNotFound) {
			// 失败次数由数据库原子递增，退避只决定下次可租约时间，不在 worker 中阻塞等待。
			delay := filestoreCleanupRetryDelay(job.Attempts + 1)
			if err := w.database.FailLeasedFilestoreObjectCleanupJob(
				ctx,
				job.ID,
				workerID,
				deleteErr.Error(),
				delay,
				filestoreCleanupMaxAttempts,
			); err != nil {
				errs = append(errs, fmt.Errorf("mark cleanup job %s retry: %w", job.ExternalID, err))
			}
			continue
		}
		if err := w.database.CompleteLeasedFilestoreObjectCleanupJob(ctx, job.ID, workerID); err != nil {
			errs = append(errs, fmt.Errorf("complete cleanup job %s: %w", job.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

// RunTTLSweepOnce 将一批到期命名空间节点原子地标记为删除，并同时创建对应的对象清理任务。
func (w *CleanupWorker) RunTTLSweepOnce(ctx context.Context) error {
	_, err := w.database.ExpireSessionNamespaceNodes(ctx, filestoreTTLSweepBatchSize)
	return err
}

func filestoreCleanupRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 6 {
		attempts = 6
	}
	return time.Duration(attempts*attempts) * time.Minute
}
