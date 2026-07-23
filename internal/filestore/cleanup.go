package filestore

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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
	LeaseFilestoreFilesystemCleanupJobs(context.Context, string, int) ([]db.FilestoreFilesystemCleanupJob, error)
	ProcessLeasedFilestoreFilesystemCleanupJob(context.Context, int64, string, int) (bool, error)
	FailLeasedFilestoreFilesystemCleanupJob(context.Context, int64, string, string, time.Duration, int) error
	LeaseFilestoreObjectCleanupJobs(context.Context, string, int) ([]db.FilestoreObjectCleanupJob, error)
	CompleteLeasedFilestoreObjectCleanupJob(context.Context, int64, string) error
	FailLeasedFilestoreObjectCleanupJob(context.Context, int64, string, string, time.Duration, int) error
	ExpireFilestoreEntries(context.Context, int) ([]db.FilestoreObjectCleanupJob, error)
}

// StartFilestoreCleanupWorker 启动单个后台循环，分别处理对象清理任务与 TTL 到期扫描。
// 两类工作都会在启动时立即执行一次，不必等待首个定时周期。
func StartFilestoreCleanupWorker(
	ctx context.Context,
	database filestoreCleanupDatabase,
	store storage.ObjectStore,
	cfg config.Config,
) {
	if database == nil || store == nil {
		return
	}
	workerID := fmt.Sprintf("filestore-cleanup-%d-%s", os.Getpid(), uuid.NewString())
	go runFilestoreCleanupLoop(ctx, database, store, cfg, workerID)
}

func runFilestoreCleanupLoop(
	ctx context.Context,
	database filestoreCleanupDatabase,
	store storage.ObjectStore,
	cfg config.Config,
	workerID string,
) {
	cleanupTicker := time.NewTicker(filestoreCleanupPollInterval)
	defer cleanupTicker.Stop()
	ttlTicker := time.NewTicker(filestoreTTLSweepInterval)
	defer ttlTicker.Stop()

	if ctx.Err() != nil {
		return
	}
	runFilestoreFilesystemCleanupAndLog(ctx, database, workerID)
	if ctx.Err() != nil {
		return
	}
	runFilestoreTTLSweepAndLog(ctx, database)
	if ctx.Err() != nil {
		return
	}
	runFilestoreCleanupAndLog(ctx, database, store, cfg, workerID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-cleanupTicker.C:
			runFilestoreFilesystemCleanupAndLog(ctx, database, workerID)
			runFilestoreCleanupAndLog(ctx, database, store, cfg, workerID)
		case <-ttlTicker.C:
			runFilestoreTTLSweepAndLog(ctx, database)
		}
	}
}

func runFilestoreFilesystemCleanupAndLog(ctx context.Context, database filestoreCleanupDatabase, workerID string) {
	if err := RunFilestoreFilesystemCleanupOnce(ctx, database, workerID); err != nil {
		log.Printf("filestore filesystem cleanup worker: %v", err)
	}
}

func runFilestoreCleanupAndLog(
	ctx context.Context,
	database filestoreCleanupDatabase,
	store storage.ObjectStore,
	cfg config.Config,
	workerID string,
) {
	if err := RunFilestoreCleanupOnce(ctx, database, store, cfg, workerID); err != nil {
		log.Printf("filestore cleanup worker: %v", err)
	}
}

func runFilestoreTTLSweepAndLog(ctx context.Context, database filestoreCleanupDatabase) {
	if err := RunFilestoreTTLSweepOnce(ctx, database); err != nil {
		log.Printf("filestore TTL sweep: %v", err)
	}
}

// RunFilestoreFilesystemCleanupOnce 把已删除 filesystem 的一批元数据转换成对象清理任务。
// 此阶段只访问数据库；真正的 S3 删除仍由对象任务在事务外完成。
func RunFilestoreFilesystemCleanupOnce(ctx context.Context, database filestoreCleanupDatabase, workerID string) error {
	jobs, err := database.LeaseFilestoreFilesystemCleanupJobs(ctx, workerID, filestoreCleanupBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		_, processErr := database.ProcessLeasedFilestoreFilesystemCleanupJob(
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
		if err := database.FailLeasedFilestoreFilesystemCleanupJob(
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

// RunFilestoreCleanupOnce 租约并处理一批有界的对象清理任务。
// 对象已不存在等同于目标已达成，按幂等成功处理。
func RunFilestoreCleanupOnce(
	ctx context.Context,
	database filestoreCleanupDatabase,
	store storage.ObjectStore,
	cfg config.Config,
	workerID string,
) error {
	jobs, err := database.LeaseFilestoreObjectCleanupJobs(ctx, workerID, filestoreCleanupBatchSize)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if job.Bucket != store.Name() {
			// 清理器绝不越过当前配置的桶边界；配置漂移时保留任务，供人工核对后重试。
			if err := database.FailLeasedFilestoreObjectCleanupJob(
				ctx,
				job.ID,
				workerID,
				"cleanup bucket does not match configured Filestore bucket",
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
			if err := database.FailLeasedFilestoreObjectCleanupJob(
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
		if err := database.CompleteLeasedFilestoreObjectCleanupJob(ctx, job.ID, workerID); err != nil {
			errs = append(errs, fmt.Errorf("complete cleanup job %s: %w", job.ExternalID, err))
		}
	}
	return errors.Join(errs...)
}

// RunFilestoreTTLSweepOnce 将一批到期条目原子地标记为删除，并同时创建对应的对象清理任务。
func RunFilestoreTTLSweepOnce(ctx context.Context, database filestoreCleanupDatabase) error {
	_, err := database.ExpireFilestoreEntries(ctx, filestoreTTLSweepBatchSize)
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
