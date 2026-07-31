package db

import (
	"context"
	"fmt"
	"math"

	"github.com/jmoiron/sqlx"
)

type workspaceStorageUsage struct {
	FilesBytes     int64 `db:"files_bytes"`
	FilestoreBytes int64 `db:"filestore_bytes"`
}

// workspaceStorageBytesQuery 从事务型账本读取工作区总用量，查询成本不随文件数量增长。
// 尚未写入过文件的新工作区可能没有账本行，此时自然视为零用量。
func workspaceStorageBytesQuery(ctx context.Context, database *sqlx.DB, workspaceUUID string) (int64, error) {
	var total int64
	err := namedGetContext(ctx, database, &total, `
		select coalesce((
			select files_bytes + filestore_bytes
			from workspace_storage_usage
			where workspace_uuid = :workspace_uuid
		), 0)
	`, map[string]any{"workspace_uuid": dbUUID(workspaceUUID)})
	return total, err
}

// GetWorkspaceStorageBytes returns the transactionally maintained Files API
// plus Filestore usage for one workspace.
func (d *DB) GetWorkspaceStorageBytes(ctx context.Context, workspaceUUID string) (int64, error) {
	return workspaceStorageBytesQuery(ctx, d.sql, workspaceUUID)
}

// ReconcileWorkspaceStorageUsage 在工作区锁内从文件事实表重建账本。
// 它用于迁移校验和低频运维修复，不应放回正常请求链路。
func (d *DB) ReconcileWorkspaceStorageUsage(ctx context.Context, workspaceUUID string) (int64, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	arguments := map[string]any{"workspace_uuid": dbUUID(workspaceUUID)}
	if _, err := namedExecContext(ctx, tx, `
		select pg_advisory_xact_lock(hashtextextended(cast(:workspace_uuid as text), 0))
	`, arguments); err != nil {
		return 0, err
	}
	usage, err := reconcileWorkspaceStorageUsageSQLXTx(ctx, tx, workspaceUUID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return usage.FilesBytes + usage.FilestoreBytes, nil
}

func reconcileWorkspaceStorageUsageSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
) (workspaceStorageUsage, error) {
	arguments := map[string]any{"workspace_uuid": dbUUID(workspaceUUID)}
	var usage workspaceStorageUsage
	if err := namedGetContext(ctx, tx, &usage, `
		select
			coalesce((
				select sum(file.size_bytes)
				from files file
				where file.workspace_uuid = :workspace_uuid
					and file.deleted_at is null
					and not exists (
					select 1
					from session_resources resource
					where resource.workspace_uuid = :workspace_uuid
							and resource.file_uuid = file.uuid
							and resource.payload is null
							and resource.deleted_at is null
					)
			), 0) as files_bytes,
			coalesce((
				select sum(file.size_bytes)
				from files file
				join session_resources resource
					on resource.file_uuid = file.uuid
					and resource.workspace_uuid = :workspace_uuid
					and resource.resource_type = 'file'
					and resource.payload is null
					and resource.deleted_at is null
				where file.workspace_uuid = :workspace_uuid and file.deleted_at is null
			), 0) as filestore_bytes
	`, arguments); err != nil {
		return workspaceStorageUsage{}, err
	}
	if usage.FilesBytes > math.MaxInt64-usage.FilestoreBytes {
		return workspaceStorageUsage{}, ErrStorageLimitExceeded
	}
	arguments["files_bytes"] = usage.FilesBytes
	arguments["filestore_bytes"] = usage.FilestoreBytes
	if _, err := namedExecContext(ctx, tx, `
		insert into workspace_storage_usage (
			workspace_uuid, files_bytes, filestore_bytes, updated_at
		)
		values (:workspace_uuid, :files_bytes, :filestore_bytes, now())
		on conflict (workspace_uuid) do update set
			files_bytes = excluded.files_bytes,
			filestore_bytes = excluded.filestore_bytes,
			updated_at = excluded.updated_at
	`, arguments); err != nil {
		return workspaceStorageUsage{}, err
	}
	return usage, nil
}

func applyWorkspaceStorageDeltaSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	filesDelta, filestoreDelta, workspaceStorageLimitBytes int64,
) error {
	arguments := map[string]any{"workspace_uuid": dbUUID(workspaceUUID)}
	if _, err := namedExecContext(ctx, tx, `
		insert into workspace_storage_usage (workspace_uuid)
		values (:workspace_uuid)
		on conflict (workspace_uuid) do nothing
	`, arguments); err != nil {
		return err
	}

	var usage workspaceStorageUsage
	if err := namedGetContext(ctx, tx, &usage, `
		select files_bytes, filestore_bytes
		from workspace_storage_usage
		where workspace_uuid = :workspace_uuid
		for update
	`, arguments); err != nil {
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
	arguments["files_bytes"] = nextFilesBytes
	arguments["filestore_bytes"] = nextFilestoreBytes
	_, err = namedExecContext(ctx, tx, `
		update workspace_storage_usage
		set files_bytes = :files_bytes,
			filestore_bytes = :filestore_bytes,
			updated_at = now()
		where workspace_uuid = :workspace_uuid
	`, arguments)
	return err
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
