package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jmoiron/sqlx"
)

var (
	leasedFilesystemCleanupJobQuery = `
		select ` + filestoreFilesystemCleanupJobColumns("j", "w", "fs") + `
		from jobs j
		join workspaces w
			on cast(w.uuid as text) = j.payload->>'workspace_uuid'
		join filestore_filesystems fs
			on cast(fs.uuid as text) = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = w.uuid
		where j.id = :job_id and j.type = :job_type and j.status = 'running'
			and j.locked_by = :lease_token and j.locked_until >= now()
		for update of j
	`
	filesystemCleanupFilesystemQuery = filestoreFilesystemSelectSQL() + `
		where uuid = :filesystem_uuid and workspace_uuid = :workspace_uuid
	`
	filesystemCleanupEntriesQuery = filestoreEntrySelectSQL() + `
		where workspace_uuid = :workspace_uuid and filesystem_uuid = :filesystem_uuid
			and kind = 'file' and deleted_at is null
		order by id
		limit :limit
		for update
	`
)

const (
	filesystemCleanupWorkspaceLockQuery = `
		select pg_advisory_xact_lock(:workspace_id)
	`
	filesystemCleanupFilesystemLockQuery = `
		select pg_advisory_xact_lock(-CAST(:filesystem_id AS bigint))
	`
	retireFilesystemCleanupEntryQuery = `
		update filestore_entries
		set deleted_at = :retired_at, updated_at = :retired_at
		where id = :entry_id and deleted_at is null
	`
	filesystemCleanupFilesRemainQuery = `
		select exists (
			select 1 from filestore_entries
			where workspace_uuid = :workspace_uuid
				and filesystem_uuid = :filesystem_uuid
				and kind = 'file' and deleted_at is null
		)
	`
	retireFilesystemCleanupDirectoriesQuery = `
		update filestore_entries
		set deleted_at = :retired_at, updated_at = :retired_at
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and kind = 'directory' and deleted_at is null
	`
	completeFilesystemCleanupBatchQuery = `
		update jobs
		set status = :status, locked_by = null, locked_until = null,
			run_after = :retired_at, updated_at = :retired_at,
			payload = payload - 'lease_attempts'
		where id = :job_id and type = :job_type and status = 'running'
			and locked_by = :lease_token
	`
)

// ExpireFilestoreEntries 原子软删除一批到期文件，并为每个失去引用的精确对象版本创建清理任务。
func (d *DB) ExpireFilestoreEntries(ctx context.Context, limit int) ([]FilestoreObjectCleanupJob, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		select distinct w.id, fs.id, oldest_expired.filesystem_uuid::text
		from (
			select workspace_uuid, filesystem_uuid, expires_at, id
			from filestore_entries
			where kind = 'file' and deleted_at is null and expires_at <= now()
			order by expires_at, id
			limit $1
		) oldest_expired
		join workspaces w on w.uuid = oldest_expired.workspace_uuid
		join filestore_filesystems fs
			on fs.uuid = oldest_expired.filesystem_uuid
			and fs.workspace_uuid = w.uuid
	`, limit)
	if err != nil {
		return nil, err
	}
	workspaceIDSet := make(map[int64]struct{})
	filesystemIDSet := make(map[int64]struct{})
	cleanupScopeByFilesystemUUID := make(map[string]filestoreEntryCleanupScope)
	var workspaceIDs []int64
	var filesystemIDs []int64
	for rows.Next() {
		var workspaceID, filesystemID int64
		var filesystemUUID string
		if err := rows.Scan(&workspaceID, &filesystemID, &filesystemUUID); err != nil {
			rows.Close()
			return nil, err
		}
		if _, found := workspaceIDSet[workspaceID]; !found {
			workspaceIDSet[workspaceID] = struct{}{}
			workspaceIDs = append(workspaceIDs, workspaceID)
		}
		if _, found := filesystemIDSet[filesystemID]; !found {
			filesystemIDSet[filesystemID] = struct{}{}
			filesystemIDs = append(filesystemIDs, filesystemID)
		}
		cleanupScopeByFilesystemUUID[filesystemUUID] = filestoreEntryCleanupScope{
			WorkspaceID: workspaceID, FilesystemID: filesystemID,
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(workspaceIDs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	sort.Slice(workspaceIDs, func(i, j int) bool {
		return workspaceIDs[i] < workspaceIDs[j]
	})
	sort.Slice(filesystemIDs, func(i, j int) bool {
		return filesystemIDs[i] < filesystemIDs[j]
	})
	// 所有容量变更都先锁工作区，再锁文件系统；批处理内部也按 ID 升序取得同类锁。
	for _, workspaceID := range workspaceIDs {
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, workspaceID); err != nil {
			return nil, err
		}
	}
	for _, filesystemID := range filesystemIDs {
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(-($1::bigint))`, filesystemID); err != nil {
			return nil, err
		}
	}

	entryRows, err := tx.Query(ctx, filestoreEntrySelectSQL()+`
		where kind = 'file' and deleted_at is null and expires_at <= now()
			and filesystem_uuid in (
				select uuid from filestore_filesystems where id = any($1::bigint[])
			)
		order by expires_at, id
		limit $2
		for update skip locked
	`, filesystemIDs, limit)
	if err != nil {
		return nil, err
	}
	entries, err := scanFilestoreEntryRowsPGX(entryRows)
	entryRows.Close()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	jobs := make([]FilestoreObjectCleanupJob, 0, len(entries))
	releasedBytesByWorkspace := make(map[int64]int64)
	for _, entry := range entries {
		scope, found := cleanupScopeByFilesystemUUID[entry.FilesystemUUID]
		if !found {
			return nil, ErrNotFound
		}
		job, err := enqueueFilestoreEntryCleanupJobPGX(ctx, tx, scope, entry, "ttl_expired", now)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
		if _, err := tx.Exec(ctx, `
			update filestore_entries set deleted_at = $2, updated_at = $2
			where id = $1 and deleted_at is null
		`, entry.ID, now); err != nil {
			return nil, err
		}
		releasedBytes, err := addWorkspaceStorageDelta(
			releasedBytesByWorkspace[scope.WorkspaceID], filestoreInt64(entry.SizeBytes),
		)
		if err != nil {
			return nil, err
		}
		releasedBytesByWorkspace[scope.WorkspaceID] = releasedBytes
	}
	for _, workspaceID := range workspaceIDs {
		releasedBytes := releasedBytesByWorkspace[workspaceID]
		if releasedBytes == 0 {
			continue
		}
		if err := applyWorkspaceStorageDeltaTx(ctx, tx, workspaceID, 0, -releasedBytes, 0); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

// LeaseFilestoreFilesystemCleanupJobs 租约一批待拆分的整文件系统清理任务。
func (d *DB) LeaseFilestoreFilesystemCleanupJobs(ctx context.Context, workerID string, limit, maxLeaseAttempts int) ([]FilestoreFilesystemCleanupJob, error) {
	var jobs []FilestoreFilesystemCleanupJob
	err := d.leaseFilestoreCleanupJobs(
		ctx,
		&jobs,
		filestoreFilesystemCleanupJobType,
		workerID,
		limit,
		maxLeaseAttempts,
		filestoreFilesystemCleanupJobColumns("j", "w", "fs"),
	)
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// ProcessLeasedFilestoreFilesystemCleanupJob 在一个短事务中退休有限数量的文件条目，
// 并把每个精确对象版本转换为既有对象清理任务。返回值表示整个文件系统是否已经退休完毕。
func (d *DB) ProcessLeasedFilestoreFilesystemCleanupJob(
	ctx context.Context,
	jobID int64,
	leaseToken string,
	limit int,
) (bool, error) {
	if strings.TrimSpace(leaseToken) == "" {
		return false, ErrPreconditionFailed
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	arguments := map[string]any{
		"job_id":      jobID,
		"job_type":    filestoreFilesystemCleanupJobType,
		"lease_token": leaseToken,
		"limit":       limit,
	}
	var job FilestoreFilesystemCleanupJob
	err = namedGetContext(ctx, tx, &job, leasedFilesystemCleanupJobQuery, arguments)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrVersionConflict
		}
		return false, err
	}
	arguments["workspace_id"] = job.WorkspaceID
	arguments["filesystem_id"] = job.FilesystemID
	arguments["workspace_uuid"] = job.WorkspaceUUID
	arguments["filesystem_uuid"] = job.FilesystemUUID
	if _, err := namedExecContext(ctx, tx, filesystemCleanupWorkspaceLockQuery, arguments); err != nil {
		return false, err
	}
	if _, err := namedExecContext(ctx, tx, filesystemCleanupFilesystemLockQuery, arguments); err != nil {
		return false, err
	}

	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filesystemCleanupFilesystemQuery, arguments)
	if err != nil {
		return false, err
	}
	cleanupScope := filestoreEntryCleanupScope{
		WorkspaceID: job.WorkspaceID, FilesystemID: filesystem.ID,
	}

	var entryRows []filestoreEntryRow
	err = namedSelectContext(ctx, tx, &entryRows, filesystemCleanupEntriesQuery, arguments)
	if err != nil {
		return false, err
	}
	entries, err := filestoreEntriesFromSQLXRows(entryRows)
	if err != nil {
		return false, err
	}

	now := time.Now().UTC()
	arguments["retired_at"] = now
	var releasedBytes int64
	for _, entry := range entries {
		if entry.SourceFileUUID == nil {
			if _, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, cleanupScope, entry, "session_deleted", now); err != nil {
				return false, err
			}
			releasedBytes, err = addWorkspaceStorageDelta(
				releasedBytes,
				filestoreInt64(entry.SizeBytes),
			)
			if err != nil {
				return false, err
			}
		}
		arguments["entry_id"] = entry.ID
		if _, err := namedExecContext(ctx, tx, retireFilesystemCleanupEntryQuery, arguments); err != nil {
			return false, err
		}
	}
	if releasedBytes > 0 {
		if err := applyWorkspaceStorageDeltaSQLXTx(ctx, tx, job.WorkspaceID, 0, -releasedBytes, 0); err != nil {
			return false, err
		}
	}

	var filesRemain bool
	if err := namedGetContext(ctx, tx, &filesRemain, filesystemCleanupFilesRemainQuery, arguments); err != nil {
		return false, err
	}
	if !filesRemain {
		if _, err := namedExecContext(ctx, tx, retireFilesystemCleanupDirectoriesQuery, arguments); err != nil {
			return false, err
		}
	}

	status := "pending"
	if !filesRemain {
		status = "completed"
	}
	arguments["status"] = status
	rowsAffected, err := namedExecRowsAffected(ctx, tx, completeFilesystemCleanupBatchQuery, arguments)
	if err != nil {
		return false, err
	}
	if rowsAffected == 0 {
		return false, ErrVersionConflict
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return !filesRemain, nil
}

// FailLeasedFilestoreFilesystemCleanupJob 记录整文件系统清理失败并按统一退避策略重试。
func (d *DB) FailLeasedFilestoreFilesystemCleanupJob(ctx context.Context, jobID int64, leaseToken, reason string, retryDelay time.Duration, maxAttempts int) error {
	return d.failLeasedFilestoreCleanupJob(
		ctx,
		jobID,
		leaseToken,
		reason,
		retryDelay,
		maxAttempts,
		filestoreFilesystemCleanupJobType,
	)
}

// EnqueueFilestoreObjectCleanupJob 持久化一条延迟对象删除任务。
func (d *DB) EnqueueFilestoreObjectCleanupJob(ctx context.Context, input EnqueueFilestoreObjectCleanupJobInput) (FilestoreObjectCleanupJob, error) {
	if input.WorkspaceID <= 0 || input.FilesystemID <= 0 ||
		strings.TrimSpace(input.Bucket) == "" || strings.TrimSpace(input.Key) == "" {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	if input.RunAfter.IsZero() {
		input.RunAfter = time.Now().UTC()
	}
	return insertFilestoreObjectCleanupJobSQLX(ctx, d.sql, input)
}

// AttachFilestoreObjectCleanupJobVersion 在文件元数据提交前记录刚上传对象的精确版本。
// 若进程随后崩溃，遗留任务仍能删除该版本，而不是在版本化桶中仅新增一个删除标记。
func (d *DB) AttachFilestoreObjectCleanupJobVersion(ctx context.Context, workspaceID int64, jobExternalID, etag, versionID string) error {
	jobExternalID = strings.TrimSpace(jobExternalID)
	if workspaceID <= 0 || jobExternalID == "" {
		return ErrPreconditionFailed
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set payload = payload || jsonb_build_object(
				'etag', cast(:etag as text),
				'version_id', cast(:version_id as text)
			),
			updated_at = now()
		where external_id = :job_external_id
			and type = :job_type
			and status in ('pending', 'retry')
			-- jobs.workspace_id 只是当前库的路由缓存；授权范围始终按稳定 UUID 判断。
			and payload->>'workspace_uuid' = (
				select cast(uuid as text) from workspaces where id = :workspace_id
			)
	`, map[string]any{
		"workspace_id":    workspaceID,
		"job_external_id": jobExternalID,
		"etag":            etag,
		"version_id":      versionID,
		"job_type":        filestoreCleanupJobType,
	})
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}
	return d.filestoreCleanupJobMutationMiss(ctx, workspaceID, jobExternalID)
}

// LeaseFilestoreObjectCleanupJobs 以 SKIP LOCKED 租约一批到期任务，允许多个 worker 并行消费。
func (d *DB) LeaseFilestoreObjectCleanupJobs(ctx context.Context, workerID string, limit, maxLeaseAttempts int) ([]FilestoreObjectCleanupJob, error) {
	var jobs []FilestoreObjectCleanupJob
	err := d.leaseFilestoreCleanupJobs(
		ctx,
		&jobs,
		filestoreCleanupJobType,
		workerID,
		limit,
		maxLeaseAttempts,
		filestoreCleanupJobColumns("j", "w", "fs"),
	)
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (d *DB) leaseFilestoreCleanupJobs(ctx context.Context, destination any, jobType, workerID string, limit, maxLeaseAttempts int, columns string) error {
	if limit <= 0 {
		limit = 10
	}
	if maxLeaseAttempts <= 0 {
		maxLeaseAttempts = 10
	}
	return namedSelectContext(ctx, d.sql, destination, `
		with exhausted_candidates as (
			select j.id
			from jobs j
			where j.type = :job_type
				and j.run_after <= now()
				and (
					j.status in ('pending', 'retry')
					or (j.status = 'running' and j.locked_until < now())
				)
				and coalesce(cast(j.payload->>'lease_attempts' as integer), 0) >= :max_lease_attempts
			order by j.run_after, j.created_at, j.id
			limit :limit
			for update of j skip locked
		),
		exhausted_jobs as (
			update jobs j
			set status = 'failed',
				locked_by = null,
				locked_until = null,
				updated_at = now(),
				payload = (j.payload - 'lease_attempts')
					|| jsonb_build_object('last_error', 'cleanup lease repeatedly expired before acknowledgement')
			from exhausted_candidates candidate
			where j.id = candidate.id
			returning j.id
		),
		next_jobs as (
			select j.id, w.id as workspace_id
			from jobs j
			join workspaces w
				on cast(w.uuid as text) = j.payload->>'workspace_uuid'
			join filestore_filesystems fs
				on cast(fs.uuid as text) = j.payload->>'filesystem_uuid'
				and fs.workspace_uuid = w.uuid
			where j.type = :job_type
				and j.run_after <= now()
				and coalesce(cast(j.payload->>'lease_attempts' as integer), 0) < :max_lease_attempts
				and not exists (select 1 from exhausted_jobs exhausted where exhausted.id = j.id)
				and (
					j.status in ('pending', 'retry')
					or (j.status = 'running' and j.locked_until < now())
				)
			order by j.run_after, j.created_at, j.id
			limit :limit
			for update of j skip locked
		),
		leased_jobs as (
			update jobs j
			set status = 'running', locked_by = :worker_id,
				locked_until = now() + interval '1 minute', updated_at = now(),
				workspace_id = next_jobs.workspace_id,
				payload = j.payload || jsonb_build_object(
					'lease_attempts',
					coalesce(cast(j.payload->>'lease_attempts' as integer), 0) + 1
				)
			from next_jobs
			where j.id = next_jobs.id
			returning j.*
		)
		select `+columns+`
		from leased_jobs j
		join workspaces w
			on cast(w.uuid as text) = j.payload->>'workspace_uuid'
		join filestore_filesystems fs
			on cast(fs.uuid as text) = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = w.uuid
	`, map[string]any{
		"job_type":           jobType,
		"limit":              limit,
		"worker_id":          workerID,
		"max_lease_attempts": maxLeaseAttempts,
	})
}

// CompleteFilestoreObjectCleanupJob 完成一条尚未出租的任务，供请求内即时补偿使用。
func (d *DB) CompleteFilestoreObjectCleanupJob(ctx context.Context, jobID int64) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = 'completed', locked_by = null, locked_until = null, updated_at = now()
		where id = :job_id and type = :job_type and status in ('pending', 'retry')
	`, map[string]any{
		"job_id":   jobID,
		"job_type": filestoreCleanupJobType,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CompleteLeasedFilestoreObjectCleanupJob 仅允许当前且未过期的租约完成任务。
func (d *DB) CompleteLeasedFilestoreObjectCleanupJob(ctx context.Context, jobID int64, leaseToken string) error {
	if strings.TrimSpace(leaseToken) == "" {
		return ErrPreconditionFailed
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = 'completed', locked_by = null, locked_until = null, updated_at = now()
		where id = :job_id and type = :job_type and status = 'running'
			and locked_by = :lease_token and locked_until >= now()
	`, map[string]any{
		"job_id":      jobID,
		"job_type":    filestoreCleanupJobType,
		"lease_token": leaseToken,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

// FailLeasedFilestoreObjectCleanupJob 记录本次失败，并在重试与终态失败之间原子推进状态。
func (d *DB) FailLeasedFilestoreObjectCleanupJob(ctx context.Context, jobID int64, leaseToken, reason string, retryDelay time.Duration, maxAttempts int) error {
	return d.failLeasedFilestoreCleanupJob(
		ctx,
		jobID,
		leaseToken,
		reason,
		retryDelay,
		maxAttempts,
		filestoreCleanupJobType,
	)
}

func (d *DB) failLeasedFilestoreCleanupJob(ctx context.Context, jobID int64, leaseToken, reason string, retryDelay time.Duration, maxAttempts int, jobType string) error {
	if strings.TrimSpace(leaseToken) == "" {
		return ErrPreconditionFailed
	}
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = case when attempts + 1 >= :max_attempts then 'failed' else 'retry' end,
			attempts = attempts + 1,
			locked_by = null,
			locked_until = null,
			run_after = :run_after,
			updated_at = now(),
			payload = (payload - 'lease_attempts')
				|| jsonb_build_object('last_error', cast(:reason as text))
		where id = :job_id and type = :job_type and status = 'running'
			and locked_by = :lease_token and locked_until >= now()
	`, map[string]any{
		"job_id":       jobID,
		"reason":       reason,
		"run_after":    runAfter,
		"max_attempts": maxAttempts,
		"job_type":     jobType,
		"lease_token":  leaseToken,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

func enqueueFilestoreFilesystemCleanupJobTx(ctx context.Context, tx pgx.Tx, filesystem FilestoreFilesystem, workspaceID int64, runAfter time.Time) (FilestoreFilesystemCleanupJob, error) {
	return scanFilestoreFilesystemCleanupJobPGX(tx.QueryRow(ctx, `
		with inserted_job as (
			insert into jobs (external_id, workspace_id, type, status, payload, run_after)
			select
				concat('job_', replace(gen_random_uuid()::text, '-', '')),
				w.id, $2, 'pending',
				jsonb_build_object(
					'workspace_uuid', w.uuid::text,
					'filesystem_uuid', fs.uuid::text
				),
				$4
			from workspaces w
			join filestore_filesystems fs
				on fs.id = $3 and fs.workspace_uuid = w.uuid
			where w.id = $1
			returning *
		)
		select `+filestoreFilesystemCleanupJobColumns("j", "w", "fs")+`
		from inserted_job j
		join workspaces w
			on w.uuid::text = j.payload->>'workspace_uuid'
		join filestore_filesystems fs
			on fs.uuid::text = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = w.uuid
	`, workspaceID, filestoreFilesystemCleanupJobType, filesystem.ID, runAfter))
}

// CancelFilestoreObjectCleanupJob 取消尚未被 worker 执行的清理任务。
func (d *DB) CancelFilestoreObjectCleanupJob(ctx context.Context, workspaceID int64, jobExternalID string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = 'canceled', locked_by = null, locked_until = null, updated_at = now()
		where external_id = :job_external_id
			and type = :job_type
			and status in ('pending', 'retry')
			and payload->>'workspace_uuid' = (
				select cast(uuid as text) from workspaces where id = :workspace_id
			)
	`, map[string]any{
		"workspace_id":    workspaceID,
		"job_external_id": jobExternalID,
		"job_type":        filestoreCleanupJobType,
	})
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}
	return d.filestoreCleanupJobMutationMiss(ctx, workspaceID, jobExternalID)
}

func (d *DB) filestoreCleanupJobMutationMiss(ctx context.Context, workspaceID int64, jobExternalID string) error {
	var status string
	err := namedGetContext(ctx, d.sql, &status, `
		select status
		from jobs
		where external_id = :job_external_id
			and type = :job_type
			and payload->>'workspace_uuid' = (
				select cast(uuid as text) from workspaces where id = :workspace_id
			)
	`, map[string]any{
		"workspace_id":    workspaceID,
		"job_external_id": jobExternalID,
		"job_type":        filestoreCleanupJobType,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return ErrFilestoreCleanupJobNotCancelable
}

type filestoreEntryCleanupScope struct {
	WorkspaceID  int64
	FilesystemID int64
}

func enqueueFilestoreEntryCleanupJobPGX(ctx context.Context, tx pgx.Tx, scope filestoreEntryCleanupScope, entry FilestoreEntry, reason string, runAfter time.Time) (FilestoreObjectCleanupJob, error) {
	if entry.Kind != FilestoreEntryKindFile || entry.S3Bucket == nil || entry.S3Key == nil {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	return enqueueFilestoreObjectCleanupJobPGX(ctx, tx, EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceID:     scope.WorkspaceID,
		FilesystemID:    scope.FilesystemID,
		EntryExternalID: entry.ExternalID,
		Bucket:          *entry.S3Bucket,
		Key:             *entry.S3Key,
		ETag:            filestoreString(entry.S3ETag),
		VersionID:       filestoreString(entry.S3VersionID),
		Reason:          reason,
		RunAfter:        runAfter,
	})
}

func enqueueFilestoreEntryCleanupJobTx(ctx context.Context, tx *sqlx.Tx, scope filestoreEntryCleanupScope, entry FilestoreEntry, reason string, runAfter time.Time) (FilestoreObjectCleanupJob, error) {
	if entry.Kind != FilestoreEntryKindFile || entry.S3Bucket == nil ||
		entry.S3Key == nil || entry.SourceFileUUID != nil {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	return insertFilestoreObjectCleanupJobSQLX(ctx, tx, EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceID:     scope.WorkspaceID,
		FilesystemID:    scope.FilesystemID,
		EntryExternalID: entry.ExternalID,
		Bucket:          *entry.S3Bucket,
		Key:             *entry.S3Key,
		ETag:            filestoreString(entry.S3ETag),
		VersionID:       filestoreString(entry.S3VersionID),
		Reason:          reason,
		RunAfter:        runAfter,
	})
}

func enqueueFilestoreSubtreeCleanupJobsTx(ctx context.Context, tx *sqlx.Tx, scope filestoreEntryCleanupScope, filesystem FilestoreFilesystem, rootPath string, runAfter time.Time) ([]FilestoreObjectCleanupJob, int64, error) {
	// rootPath 本身是目录，文件只可能出现在严格后代中；分隔符比较避免同前缀误选。
	var rows []filestoreEntryRow
	err := namedSelectContext(ctx, tx, &rows, filestoreEntrySelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and kind = 'file'
			and deleted_at is null
			and left(path, char_length(:root_path) + 1) = :root_path || '/'
		order by id
		for update
	`, filestoreSubtreeArguments(filesystem, rootPath))
	if err != nil {
		return nil, 0, err
	}
	entries, err := filestoreEntriesFromSQLXRows(rows)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]FilestoreObjectCleanupJob, 0, len(entries))
	var removedBytes int64
	for _, entry := range entries {
		job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, scope, entry, "remove_directory", runAfter)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, job)
		removedBytes, err = addWorkspaceStorageDelta(removedBytes, filestoreInt64(entry.SizeBytes))
		if err != nil {
			return nil, 0, err
		}
	}
	return jobs, removedBytes, nil
}

func retireExpiredFilestoreSubtreeTx(
	ctx context.Context,
	tx *sqlx.Tx,
	scope filestoreEntryCleanupScope,
	filesystem FilestoreFilesystem,
	rootPath string,
	retiredAt time.Time,
) ([]FilestoreObjectCleanupJob, int64, error) {
	var rows []filestoreEntryRow
	err := namedSelectContext(ctx, tx, &rows, filestoreEntrySelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and kind = 'file'
			and deleted_at is null and expires_at <= now()
			and (
				path = :root_path
				or left(path, char_length(:root_path) + 1) = :root_path || '/'
			)
		order by id
		for update
	`, filestoreSubtreeArguments(filesystem, rootPath))
	if err != nil {
		return nil, 0, err
	}
	entries, err := filestoreEntriesFromSQLXRows(rows)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]FilestoreObjectCleanupJob, 0, len(entries))
	var retiredBytes int64
	for _, entry := range entries {
		job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, scope, entry, "expired_destination_replaced", retiredAt)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, job)
		retiredBytes, err = addWorkspaceStorageDelta(retiredBytes, filestoreInt64(entry.SizeBytes))
		if err != nil {
			return nil, 0, err
		}
		if _, err := namedExecContext(ctx, tx, `
			update filestore_entries
			set deleted_at = :retired_at, updated_at = :retired_at
			where id = :entry_id and deleted_at is null
		`, map[string]any{
			"entry_id":   entry.ID,
			"retired_at": retiredAt,
		}); err != nil {
			return nil, 0, err
		}
	}
	return jobs, retiredBytes, nil
}

func filestoreSubtreeArguments(filesystem FilestoreFilesystem, rootPath string) map[string]any {
	return map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"root_path":       rootPath,
	}
}

func enqueueFilestoreObjectCleanupJobPGX(ctx context.Context, q filestorePGXQueryRower, input EnqueueFilestoreObjectCleanupJobInput) (FilestoreObjectCleanupJob, error) {
	return scanFilestoreCleanupJobPGX(q.QueryRow(ctx, `
		with inserted_job as (
			insert into jobs (external_id, workspace_id, type, status, payload, run_after)
			select
				concat('job_', replace(gen_random_uuid()::text, '-', '')),
				w.id, $2, 'pending',
				jsonb_build_object(
					'workspace_uuid', w.uuid::text,
					'filesystem_uuid', fs.uuid::text,
					'entry_external_id', $4::text,
					'bucket', $5::text,
					'key', $6::text,
					'etag', $7::text,
					'version_id', $8::text,
					'reason', $9::text
				),
				$10
			from workspaces w
			join filestore_filesystems fs
				on fs.id = $3 and fs.workspace_uuid = w.uuid
			where w.id = $1
			returning *
		)
		select `+filestoreCleanupJobColumns("j", "w", "fs")+`
		from inserted_job j
		join workspaces w
			on w.uuid::text = j.payload->>'workspace_uuid'
		join filestore_filesystems fs
			on fs.uuid::text = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = w.uuid
	`, input.WorkspaceID, filestoreCleanupJobType, input.FilesystemID,
		input.EntryExternalID, input.Bucket, input.Key,
		input.ETag, input.VersionID, input.Reason, input.RunAfter))
}

func cancelAttachedFilestoreObjectCleanupJobTx(ctx context.Context, tx *sqlx.Tx, workspaceID int64, jobExternalID string, blob FilestoreFileBlob) error {
	// 将哨兵取消与文件条目提交置于同一事务；任一失败都会保留可重试的清理路径。
	rowsAffected, err := namedExecRowsAffected(ctx, tx, `
		update jobs
		set status = 'canceled', locked_by = null, locked_until = null, updated_at = now()
		where external_id = :job_external_id and type = :job_type
			and status in ('pending', 'retry')
			and payload->>'workspace_uuid' = (
				select CAST(uuid AS text) from workspaces where id = :workspace_id
			)
			and payload->>'bucket' = :bucket
			and payload->>'key' = :key
			and coalesce(payload->>'version_id', '') = :version_id
	`, map[string]any{
		"workspace_id":    workspaceID,
		"job_external_id": jobExternalID,
		"job_type":        filestoreCleanupJobType,
		"bucket":          blob.S3Bucket,
		"key":             blob.S3Key,
		"version_id":      blob.S3VersionID,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrFilestoreCleanupJobNotCancelable
	}
	return nil
}

func filestoreCleanupJobColumns(jobAlias, workspaceAlias, filesystemAlias string) string {
	return fmt.Sprintf(`%[1]s.id as id, %[1]s.external_id as external_id,
		cast(%[2]s.uuid as text) as workspace_uuid,
		cast(%[3]s.uuid as text) as filesystem_uuid,
		%[2]s.id as workspace_id, %[3]s.id as filesystem_id,
		%[3]s.external_id as filesystem_external_id,
		coalesce(%[1]s.payload->>'entry_external_id', '') as entry_external_id,
		coalesce(%[1]s.payload->>'bucket', '') as bucket,
		coalesce(%[1]s.payload->>'key', '') as key,
		coalesce(%[1]s.payload->>'etag', '') as etag,
		coalesce(%[1]s.payload->>'version_id', '') as version_id,
		coalesce(%[1]s.payload->>'reason', '') as reason,
		%[1]s.attempts as attempts, %[1]s.run_after as run_after`,
		jobAlias, workspaceAlias, filesystemAlias)
}

func filestoreFilesystemCleanupJobColumns(jobAlias, workspaceAlias, filesystemAlias string) string {
	return fmt.Sprintf(`%[1]s.id as id, %[1]s.external_id as external_id,
		cast(%[2]s.uuid as text) as workspace_uuid,
		cast(%[3]s.uuid as text) as filesystem_uuid,
		%[2]s.id as workspace_id, %[3]s.id as filesystem_id,
		%[3]s.external_id as filesystem_external_id,
		%[1]s.attempts as attempts, %[1]s.run_after as run_after`,
		jobAlias, workspaceAlias, filesystemAlias)
}

func scanFilestoreCleanupJobPGX(row filestorePGXScanner) (FilestoreObjectCleanupJob, error) {
	var job FilestoreObjectCleanupJob
	err := row.Scan(&job.ID, &job.ExternalID, &job.WorkspaceUUID, &job.FilesystemUUID,
		&job.WorkspaceID, &job.FilesystemID,
		&job.FilesystemExternalID, &job.EntryExternalID, &job.Bucket, &job.Key,
		&job.ETag, &job.VersionID, &job.Reason, &job.Attempts, &job.RunAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return FilestoreObjectCleanupJob{}, ErrNotFound
	}
	return job, err
}

func scanFilestoreFilesystemCleanupJobPGX(row filestorePGXScanner) (FilestoreFilesystemCleanupJob, error) {
	var job FilestoreFilesystemCleanupJob
	err := row.Scan(
		&job.ID,
		&job.ExternalID,
		&job.WorkspaceUUID,
		&job.FilesystemUUID,
		&job.WorkspaceID,
		&job.FilesystemID,
		&job.FilesystemExternalID,
		&job.Attempts,
		&job.RunAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return FilestoreFilesystemCleanupJob{}, ErrNotFound
	}
	return job, err
}
