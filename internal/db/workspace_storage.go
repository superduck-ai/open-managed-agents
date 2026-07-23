package db

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jmoiron/sqlx"
)

type workspaceStorageUsage struct {
	filesBytes     int64
	filestoreBytes int64
}

// workspaceStorageBytesQuery 从事务型账本读取工作区总用量，查询成本不随文件数量增长。
// 尚未写入过文件的新工作区可能没有账本行，此时自然视为零用量。
func workspaceStorageBytesQuery(ctx context.Context, database *sqlx.DB, workspaceID int64) (int64, error) {
	var total int64
	err := namedGetContext(ctx, database, &total, `
		select coalesce((
			select files_bytes + filestore_bytes
			from workspace_storage_usage
			where workspace_id = :workspace_id
		), 0)
	`, map[string]any{"workspace_id": workspaceID})
	return total, err
}

// ReconcileWorkspaceStorageUsage 在工作区锁内从文件事实表重建账本。
// 它用于迁移校验和低频运维修复，不应放回正常请求链路。
func (d *DB) ReconcileWorkspaceStorageUsage(ctx context.Context, workspaceID int64) (int64, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, workspaceID); err != nil {
		return 0, err
	}

	var usage workspaceStorageUsage
	if err := tx.QueryRow(ctx, `
		select
			coalesce((
				select sum(size_bytes) from files
				where workspace_id = $1 and deleted_at is null
			), 0),
			coalesce((
				select sum(size_bytes) from filestore_entries
				where workspace_uuid = (select uuid from workspaces where id = $1)
					and kind = 'file' and deleted_at is null
			), 0)
	`, workspaceID).Scan(&usage.filesBytes, &usage.filestoreBytes); err != nil {
		return 0, err
	}
	if usage.filesBytes > math.MaxInt64-usage.filestoreBytes {
		return 0, ErrStorageLimitExceeded
	}
	if _, err := tx.Exec(ctx, `
		insert into workspace_storage_usage (
			workspace_id, files_bytes, filestore_bytes, updated_at
		)
		values ($1, $2, $3, now())
		on conflict (workspace_id) do update set
			files_bytes = excluded.files_bytes,
			filestore_bytes = excluded.filestore_bytes,
			updated_at = excluded.updated_at
	`, workspaceID, usage.filesBytes, usage.filestoreBytes); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return usage.filesBytes + usage.filestoreBytes, nil
}

// applyWorkspaceStorageDeltaTx 在资源事务内维护账本，并在增加用量时执行共享配额检查。
// 调用方必须先持有正数 workspace advisory lock，使 Files 与 Filestore 共用串行点。
func applyWorkspaceStorageDeltaTx(
	ctx context.Context,
	tx pgx.Tx,
	workspaceID, filesDelta, filestoreDelta, workspaceStorageLimitBytes int64,
) error {
	if _, err := tx.Exec(ctx, `
		insert into workspace_storage_usage (workspace_id)
		values ($1)
		on conflict (workspace_id) do nothing
	`, workspaceID); err != nil {
		return err
	}

	var usage workspaceStorageUsage
	if err := tx.QueryRow(ctx, `
		select files_bytes, filestore_bytes
		from workspace_storage_usage
		where workspace_id = $1
		for update
	`, workspaceID).Scan(&usage.filesBytes, &usage.filestoreBytes); err != nil {
		return err
	}

	nextFilesBytes, err := addWorkspaceStorageDelta(usage.filesBytes, filesDelta)
	if err != nil {
		return fmt.Errorf("update workspace %d Files API storage usage: %w", workspaceID, err)
	}
	nextFilestoreBytes, err := addWorkspaceStorageDelta(usage.filestoreBytes, filestoreDelta)
	if err != nil {
		return fmt.Errorf("update workspace %d Filestore storage usage: %w", workspaceID, err)
	}
	if nextFilesBytes > math.MaxInt64-nextFilestoreBytes {
		return ErrStorageLimitExceeded
	}
	nextTotal := nextFilesBytes + nextFilestoreBytes
	if workspaceStorageLimitBytes > 0 && nextTotal > workspaceStorageLimitBytes {
		return ErrStorageLimitExceeded
	}

	_, err = tx.Exec(ctx, `
		update workspace_storage_usage
		set files_bytes = $2, filestore_bytes = $3, updated_at = now()
		where workspace_id = $1
	`, workspaceID, nextFilesBytes, nextFilestoreBytes)
	return err
}

func addWorkspaceStorageDelta(current, delta int64) (int64, error) {
	if delta > 0 && current > math.MaxInt64-delta {
		return 0, ErrStorageLimitExceeded
	}
	if delta == math.MinInt64 || delta < 0 && current < -delta {
		return 0, fmt.Errorf("usage counter underflow: current=%d delta=%d", current, delta)
	}
	return current + delta, nil
}
