package db

import (
	"context"
	"fmt"
	"math"

	"github.com/superduck-ai/yourbatis"
)

type workspaceStorageUsage struct {
	FilesBytes     int64 `db:"files_bytes"`
	FilestoreBytes int64 `db:"filestore_bytes"`
}

// GetWorkspaceStorageBytes returns the transactionally maintained Files API
// plus Filestore usage for one workspace. The query reads the transactional
// ledger, so its cost does not grow with the number of files. A workspace with
// no ledger row is treated as using zero bytes.
func (d *DB) GetWorkspaceStorageBytes(ctx context.Context, workspaceUUID string) (int64, error) {
	mapper := NewWorkspaceStorageUsageMapper(d.mapperDB)
	return mapper.GetWorkspaceStorageBytes(ctx, workspaceUUID)
}

// ReconcileWorkspaceStorageUsage 在工作区锁内从文件事实表重建账本。
// 它用于迁移校验和低频运维修复，不应放回正常请求链路。
func (d *DB) ReconcileWorkspaceStorageUsage(ctx context.Context, workspaceUUID string) (int64, error) {
	var usage workspaceStorageUsage
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewWorkspaceStorageUsageMapper(executor)
		if err := mapper.LockWorkspace(ctx, workspaceUUID); err != nil {
			return err
		}
		var err error
		usage, err = reconcileWorkspaceStorageUsageTx(ctx, executor, workspaceUUID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return usage.FilesBytes + usage.FilestoreBytes, nil
}

func reconcileWorkspaceStorageUsageTx(
	ctx context.Context,
	tx yourbatis.Executor,
	workspaceUUID string,
) (workspaceStorageUsage, error) {
	mapper := NewWorkspaceStorageUsageMapper(tx)
	usage, err := mapper.ReconcileWorkspaceStorageUsage(ctx, workspaceUUID)
	if err != nil {
		return workspaceStorageUsage{}, err
	}
	if usage.FilesBytes > math.MaxInt64-usage.FilestoreBytes {
		return workspaceStorageUsage{}, ErrStorageLimitExceeded
	}
	if err := mapper.UpsertWorkspaceStorageUsage(ctx, workspaceStorageUsageParams{
		WorkspaceUUID:  workspaceUUID,
		FilesBytes:     usage.FilesBytes,
		FilestoreBytes: usage.FilestoreBytes,
	}); err != nil {
		return workspaceStorageUsage{}, err
	}
	return usage, nil
}

func applyWorkspaceStorageDeltaTx(
	ctx context.Context,
	tx yourbatis.Executor,
	workspaceUUID string,
	filesDelta, filestoreDelta, workspaceStorageLimitBytes int64,
) error {
	mapper := NewWorkspaceStorageUsageMapper(tx)
	if err := mapper.EnsureWorkspaceStorageUsage(ctx, workspaceUUID); err != nil {
		return err
	}

	usage, err := mapper.GetWorkspaceStorageUsageForUpdate(ctx, workspaceUUID)
	if err != nil {
		return err
	}
	nextFilesBytes, nextFilestoreBytes, err := nextWorkspaceStorageUsage(
		workspaceUUID,
		usage,
		filesDelta,
		filestoreDelta,
		workspaceStorageLimitBytes,
	)
	if err != nil {
		return err
	}
	return mapper.UpdateWorkspaceStorageUsage(ctx, workspaceStorageUsageParams{
		WorkspaceUUID:  workspaceUUID,
		FilesBytes:     nextFilesBytes,
		FilestoreBytes: nextFilestoreBytes,
	})
}

func nextWorkspaceStorageUsage(
	workspaceUUID string,
	usage workspaceStorageUsage,
	filesDelta, filestoreDelta, workspaceStorageLimitBytes int64,
) (int64, int64, error) {
	nextFilesBytes, err := addWorkspaceStorageDelta(usage.FilesBytes, filesDelta)
	if err != nil {
		return 0, 0, fmt.Errorf("update workspace %s Files API storage usage: %w", workspaceUUID, err)
	}
	nextFilestoreBytes, err := addWorkspaceStorageDelta(usage.FilestoreBytes, filestoreDelta)
	if err != nil {
		return 0, 0, fmt.Errorf("update workspace %s Filestore storage usage: %w", workspaceUUID, err)
	}
	if nextFilesBytes > math.MaxInt64-nextFilestoreBytes {
		return 0, 0, ErrStorageLimitExceeded
	}
	nextTotal := nextFilesBytes + nextFilestoreBytes
	if workspaceStorageLimitBytes > 0 && nextTotal > workspaceStorageLimitBytes {
		return 0, 0, ErrStorageLimitExceeded
	}
	return nextFilesBytes, nextFilestoreBytes, nil
}

func addWorkspaceStorageDelta(current, delta int64) (int64, error) {
	if delta > 0 && current > math.MaxInt64-delta {
		return 0, ErrStorageLimitExceeded
	}
	if delta == math.MinInt64 || delta < 0 && current < -delta {
		return 0, fmt.Errorf("%w: current=%d delta=%d", ErrStorageUsageUnderflow, current, delta)
	}
	return current + delta, nil
}
