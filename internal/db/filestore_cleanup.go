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
		select distinct w.id, fs.id, fs.external_id, oldest_expired.filesystem_uuid::text
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
		var filesystemExternalID, filesystemUUID string
		if err := rows.Scan(&workspaceID, &filesystemID, &filesystemExternalID, &filesystemUUID); err != nil {
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
			FilesystemExternalID: filesystemExternalID,
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
		job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, scope, entry, "ttl_expired", now)
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
func (d *DB) LeaseFilestoreFilesystemCleanupJobs(ctx context.Context, workerID string, limit int) ([]FilestoreFilesystemCleanupJob, error) {
	var jobs []FilestoreFilesystemCleanupJob
	err := d.leaseFilestoreCleanupJobs(
		ctx,
		&jobs,
		filestoreFilesystemCleanupJobType,
		workerID,
		limit,
		filestoreFilesystemCleanupJobColumns("j"),
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

	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	job, err := scanFilestoreFilesystemCleanupJobPGX(tx.QueryRow(ctx, `
		select `+filestoreFilesystemCleanupJobColumns("")+`
		from jobs
		where id = $1 and type = $2 and status = 'running'
			and locked_by = $3 and locked_until >= now()
		for update
	`, jobID, filestoreFilesystemCleanupJobType, leaseToken))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return false, ErrVersionConflict
		}
		return false, err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, job.WorkspaceID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(-($1::bigint))`, job.FilesystemID); err != nil {
		return false, err
	}

	filesystem, err := scanFilestoreFilesystemPGX(tx.QueryRow(ctx, filestoreFilesystemSelectSQL()+`
		where id = $1
			and workspace_uuid = (select uuid from workspaces where id = $2)
	`, job.FilesystemID, job.WorkspaceID))
	if err != nil {
		return false, err
	}
	cleanupScope := filestoreEntryCleanupScope{
		WorkspaceID: job.WorkspaceID, FilesystemID: filesystem.ID,
		FilesystemExternalID: filesystem.ExternalID,
	}

	entryRows, err := tx.Query(ctx, filestoreEntrySelectSQL()+`
		where workspace_uuid = $1 and filesystem_uuid = $2
			and kind = 'file' and deleted_at is null
		order by id
		limit $3
		for update
	`, filesystem.WorkspaceUUID, filesystem.UUID, limit)
	if err != nil {
		return false, err
	}
	entries, err := scanFilestoreEntryRowsPGX(entryRows)
	entryRows.Close()
	if err != nil {
		return false, err
	}

	now := time.Now().UTC()
	var releasedBytes int64
	for _, entry := range entries {
		if _, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, cleanupScope, entry, "session_deleted", now); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `
			update filestore_entries
			set deleted_at = $2, updated_at = $2
			where id = $1 and deleted_at is null
		`, entry.ID, now); err != nil {
			return false, err
		}
		releasedBytes, err = addWorkspaceStorageDelta(releasedBytes, filestoreInt64(entry.SizeBytes))
		if err != nil {
			return false, err
		}
	}
	if releasedBytes > 0 {
		if err := applyWorkspaceStorageDeltaTx(ctx, tx, job.WorkspaceID, 0, -releasedBytes, 0); err != nil {
			return false, err
		}
	}

	var filesRemain bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from filestore_entries
			where workspace_uuid = $1 and filesystem_uuid = $2
				and kind = 'file' and deleted_at is null
		)
	`, filesystem.WorkspaceUUID, filesystem.UUID).Scan(&filesRemain); err != nil {
		return false, err
	}
	if !filesRemain {
		if _, err := tx.Exec(ctx, `
			update filestore_entries
			set deleted_at = $3, updated_at = $3
			where workspace_uuid = $1 and filesystem_uuid = $2
				and kind = 'directory' and deleted_at is null
		`, filesystem.WorkspaceUUID, filesystem.UUID, now); err != nil {
			return false, err
		}
	}

	status := "pending"
	if !filesRemain {
		status = "completed"
	}
	tag, err := tx.Exec(ctx, `
		update jobs
		set status = $4, locked_by = null, locked_until = null,
			run_after = $5, updated_at = $5
		where id = $1 and type = $2 and status = 'running' and locked_by = $3
	`, job.ID, filestoreFilesystemCleanupJobType, leaseToken, status, now)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, ErrVersionConflict
	}
	if err := tx.Commit(ctx); err != nil {
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
	if input.WorkspaceID <= 0 || strings.TrimSpace(input.Bucket) == "" || strings.TrimSpace(input.Key) == "" {
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
		where workspace_id = :workspace_id
			and external_id = :job_external_id
			and type = :job_type
			and status in ('pending', 'retry')
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
func (d *DB) LeaseFilestoreObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]FilestoreObjectCleanupJob, error) {
	var jobs []FilestoreObjectCleanupJob
	err := d.leaseFilestoreCleanupJobs(ctx, &jobs, filestoreCleanupJobType, workerID, limit, filestoreCleanupJobColumns("j"))
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (d *DB) leaseFilestoreCleanupJobs(ctx context.Context, destination any, jobType, workerID string, limit int, columns string) error {
	if limit <= 0 {
		limit = 10
	}
	return namedSelectContext(ctx, d.sql, destination, `
		with next_jobs as (
			select id
			from jobs
			where type = :job_type
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at, id
			limit :limit
			for update skip locked
		)
		update jobs j
		set status = 'running', locked_by = :worker_id,
			locked_until = now() + interval '1 minute', updated_at = now()
		from next_jobs
		where j.id = next_jobs.id
		returning `+columns+`
	`, map[string]any{
		"job_type":  jobType,
		"limit":     limit,
		"worker_id": workerID,
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
			payload = payload || jsonb_build_object('last_error', cast(:reason as text))
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
		insert into jobs (external_id, workspace_id, type, status, payload, run_after)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1, $2, 'pending',
			jsonb_build_object(
				'filesystem_id', $3::bigint,
				'filesystem_external_id', $4::text
			),
			$5
		)
		returning `+filestoreFilesystemCleanupJobColumns("jobs")+`
	`, workspaceID, filestoreFilesystemCleanupJobType, filesystem.ID, filesystem.ExternalID, runAfter))
}

// CancelFilestoreObjectCleanupJob 取消尚未被 worker 执行的清理任务。
func (d *DB) CancelFilestoreObjectCleanupJob(ctx context.Context, workspaceID int64, jobExternalID string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = 'canceled', locked_by = null, locked_until = null, updated_at = now()
		where workspace_id = :workspace_id
			and external_id = :job_external_id
			and type = :job_type
			and status in ('pending', 'retry')
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
		where workspace_id = :workspace_id
			and external_id = :job_external_id
			and type = :job_type
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
	WorkspaceID          int64
	FilesystemID         int64
	FilesystemExternalID string
}

func enqueueFilestoreEntryCleanupJobTx(ctx context.Context, tx pgx.Tx, scope filestoreEntryCleanupScope, entry FilestoreEntry, reason string, runAfter time.Time) (FilestoreObjectCleanupJob, error) {
	if entry.Kind != FilestoreEntryKindFile || entry.S3Bucket == nil || entry.S3Key == nil {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	return enqueueFilestoreObjectCleanupJobPGX(ctx, tx, EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceID:          scope.WorkspaceID,
		FilesystemID:         scope.FilesystemID,
		FilesystemExternalID: scope.FilesystemExternalID,
		EntryExternalID:      entry.ExternalID,
		Bucket:               *entry.S3Bucket,
		Key:                  *entry.S3Key,
		ETag:                 filestoreString(entry.S3ETag),
		VersionID:            filestoreString(entry.S3VersionID),
		Reason:               reason,
		RunAfter:             runAfter,
	})
}

func enqueueFilestoreSubtreeCleanupJobsTx(ctx context.Context, tx pgx.Tx, scope filestoreEntryCleanupScope, filesystem FilestoreFilesystem, rootPath string, runAfter time.Time) ([]FilestoreObjectCleanupJob, int64, error) {
	// rootPath 本身是目录，文件只可能出现在严格后代中；分隔符比较避免同前缀误选。
	rows, err := tx.Query(ctx, filestoreEntrySelectSQL()+`
		where workspace_uuid = $1 and filesystem_uuid = $2 and kind = 'file' and deleted_at is null
			and left(path, char_length($3) + 1) = $3 || '/'
		order by id
		for update
	`, filesystem.WorkspaceUUID, filesystem.UUID, rootPath)
	if err != nil {
		return nil, 0, err
	}
	entries, err := scanFilestoreEntryRowsPGX(rows)
	rows.Close()
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
	tx pgx.Tx,
	scope filestoreEntryCleanupScope,
	filesystem FilestoreFilesystem,
	rootPath string,
	retiredAt time.Time,
) ([]FilestoreObjectCleanupJob, int64, error) {
	rows, err := tx.Query(ctx, filestoreEntrySelectSQL()+`
		where workspace_uuid = $1 and filesystem_uuid = $2 and kind = 'file'
			and deleted_at is null and expires_at <= now()
			and (path = $3 or left(path, char_length($3) + 1) = $3 || '/')
		order by id
		for update
	`, filesystem.WorkspaceUUID, filesystem.UUID, rootPath)
	if err != nil {
		return nil, 0, err
	}
	entries, err := scanFilestoreEntryRowsPGX(rows)
	rows.Close()
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
		if _, err := tx.Exec(ctx, `
			update filestore_entries set deleted_at = $2, updated_at = $2
			where id = $1 and deleted_at is null
		`, entry.ID, retiredAt); err != nil {
			return nil, 0, err
		}
	}
	return jobs, retiredBytes, nil
}

func enqueueFilestoreObjectCleanupJobPGX(ctx context.Context, q filestorePGXQueryRower, input EnqueueFilestoreObjectCleanupJobInput) (FilestoreObjectCleanupJob, error) {
	return scanFilestoreCleanupJobPGX(q.QueryRow(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload, run_after)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1, $2, 'pending',
			jsonb_build_object(
				'filesystem_id', $3::bigint,
				'filesystem_external_id', $4::text,
				'entry_external_id', $5::text,
				'bucket', $6::text,
				'key', $7::text,
				'etag', $8::text,
				'version_id', $9::text,
				'reason', $10::text
			),
			$11
		)
		returning `+filestoreCleanupJobColumns("jobs")+`
	`, input.WorkspaceID, filestoreCleanupJobType, input.FilesystemID,
		input.FilesystemExternalID, input.EntryExternalID, input.Bucket, input.Key,
		input.ETag, input.VersionID, input.Reason, input.RunAfter))
}

func cancelAttachedFilestoreObjectCleanupJobTx(ctx context.Context, tx pgx.Tx, workspaceID int64, jobExternalID string, blob FilestoreFileBlob) error {
	// 将哨兵取消与文件条目提交置于同一事务；任一失败都会保留可重试的清理路径。
	tag, err := tx.Exec(ctx, `
		update jobs
		set status = 'canceled', locked_by = null, locked_until = null, updated_at = now()
		where workspace_id = $1 and external_id = $2 and type = $3
			and status in ('pending', 'retry')
			and payload->>'bucket' = $4
			and payload->>'key' = $5
			and coalesce(payload->>'version_id', '') = $6
	`, workspaceID, jobExternalID, filestoreCleanupJobType, blob.S3Bucket, blob.S3Key, blob.S3VersionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrFilestoreCleanupJobNotCancelable
	}
	return nil
}

func filestoreCleanupJobColumns(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return fmt.Sprintf(`%[1]sid as id, %[1]sexternal_id as external_id,
		%[1]sworkspace_id as workspace_id,
		coalesce(cast(%[1]spayload->>'filesystem_id' as bigint), 0) as filesystem_id,
		coalesce(%[1]spayload->>'filesystem_external_id', '') as filesystem_external_id,
		coalesce(%[1]spayload->>'entry_external_id', '') as entry_external_id,
		coalesce(%[1]spayload->>'bucket', '') as bucket,
		coalesce(%[1]spayload->>'key', '') as key,
		coalesce(%[1]spayload->>'etag', '') as etag,
		coalesce(%[1]spayload->>'version_id', '') as version_id,
		coalesce(%[1]spayload->>'reason', '') as reason,
		%[1]sattempts as attempts, %[1]srun_after as run_after`, prefix)
}

func filestoreFilesystemCleanupJobColumns(alias string) string {
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	return fmt.Sprintf(`%[1]sid as id, %[1]sexternal_id as external_id,
		%[1]sworkspace_id as workspace_id,
		coalesce(cast(%[1]spayload->>'filesystem_id' as bigint), 0) as filesystem_id,
		coalesce(%[1]spayload->>'filesystem_external_id', '') as filesystem_external_id,
		%[1]sattempts as attempts, %[1]srun_after as run_after`, prefix)
}

func scanFilestoreCleanupJobPGX(row filestorePGXScanner) (FilestoreObjectCleanupJob, error) {
	var job FilestoreObjectCleanupJob
	err := row.Scan(&job.ID, &job.ExternalID, &job.WorkspaceID, &job.FilesystemID,
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
