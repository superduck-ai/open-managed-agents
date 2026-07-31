package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

var (
	enqueueFilestoreFilesystemCleanupJobQuery = `
		with inserted_job as (
			insert into jobs (external_id, workspace_id, type, status, payload, run_after)
			values (
				concat('job_', replace(CAST(gen_random_uuid() AS text), '-', '')),
				:workspace_uuid, :job_type, 'pending',
				jsonb_build_object(
					'workspace_uuid', CAST(:workspace_uuid AS text),
					'filesystem_uuid', CAST(:filesystem_uuid AS text)
				),
				:run_after
			)
			returning *
		)
		select ` + filestoreFilesystemCleanupJobColumns("j", "fs") + `
		from inserted_job j
		join filestore_filesystems fs
			on CAST(fs.uuid AS text) = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = j.workspace_uuid
	`
	leasedFilesystemCleanupJobQuery = `
		select ` + filestoreFilesystemCleanupJobColumns("j", "fs") + `
		from jobs j
		join filestore_filesystems fs
			on cast(fs.uuid as text) = j.payload->>'filesystem_uuid'
			and fs.workspace_uuid = j.workspace_uuid
		where j.uuid = :job_uuid and j.type = :job_type and j.status = 'running'
			and j.locked_by = :lease_token and j.locked_until >= now()
		for update of j
	`
	filesystemCleanupFilesystemQuery = filestoreFilesystemSelectSQL() + `
		where uuid = :filesystem_uuid and workspace_uuid = :workspace_uuid
	`
	filesystemCleanupEntriesQuery = sessionResourceFileSelectSQL() + `
		where workspace_uuid = :workspace_uuid and session_uuid = :session_uuid
			and kind = 'file' and deleted_at is null
		order by id
		limit :limit
	`
	expiredSessionResourceFilesQuery = sessionResourceFileSelectSQL() + `
		where kind = 'file' and deleted_at is null and expires_at <= now()
			and (workspace_uuid, session_uuid) in (
				select workspace_uuid, session_uuid
				from filestore_filesystems
				where uuid = any(:filesystem_uuids)
			)
		order by expires_at, uuid
		limit :limit
	`
)

const (
	expiredFilestoreCleanupScopesQuery = `
		select distinct oldest_expired.workspace_uuid,
			filesystem.uuid AS filesystem_uuid,
			oldest_expired.session_uuid
		from (
			select workspace_uuid, session_uuid, expires_at, uuid
			from session_resources
			where resource_type = 'file' and deleted_at is null and expires_at <= now()
			order by expires_at, uuid
			limit :limit
		) oldest_expired
		join filestore_filesystems filesystem
			on filesystem.session_uuid = oldest_expired.session_uuid
			and filesystem.workspace_uuid = oldest_expired.workspace_uuid
			and filesystem.deleted_at is null
	`
	filesystemCleanupWorkspaceLockQuery = `
		select pg_advisory_xact_lock(hashtextextended(CAST(:workspace_uuid AS text), 0))
	`
	filesystemCleanupFilesystemLockQuery = `
		select pg_advisory_xact_lock(hashtextextended(concat('filestore-filesystem', chr(58), CAST(:filesystem_uuid AS text)), 0))
	`
	filesystemCleanupFilesRemainQuery = `
		select exists (
			select 1 from session_resources
			where workspace_uuid = :workspace_uuid
				and session_uuid = :session_uuid
				and resource_type = 'file' and deleted_at is null
		)
	`
	retireFilesystemCleanupNamespaceEntriesQuery = `
		update session_resources
		set deleted_at = :retired_at, updated_at = :retired_at
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and resource_type in ('directory', 'skill_archive') and deleted_at is null
	`
	retireFilesystemCleanupSkillFilesQuery = `
		update files file
		set deleted_at = :retired_at
		where file.workspace_uuid = :workspace_uuid
			and file.deleted_at is null
			and file.uuid in (
				select resource.file_uuid
				from session_resources resource
				where resource.workspace_uuid = (
					:workspace_uuid
				)
					and resource.session_uuid = :session_uuid
					and resource.resource_type = 'skill_archive'
					and resource.file_uuid is not null
					and resource.deleted_at is null
			)
	`
	completeFilesystemCleanupBatchQuery = `
		update jobs
		set status = :status, locked_by = null, locked_until = null,
			run_after = :retired_at, updated_at = :retired_at,
			payload = payload - 'lease_attempts'
		where uuid = :job_uuid and type = :job_type and status = 'running'
			and locked_by = :lease_token
	`
)

// ExpireSessionResourceFiles 原子软删除一批到期文件，并为每个失去引用的精确对象版本创建清理任务。
// 无法定位对象的异常节点仍会被退休并通过 anomalies 返回给 worker 记录。
func (d *DB) ExpireSessionResourceFiles(ctx context.Context, limit int) ([]FilestoreObjectCleanupJob, []FilestoreCleanupAnomaly, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var scopeRows []expiredFilestoreCleanupScopeRow
	err = namedSelectContext(ctx, tx, &scopeRows, expiredFilestoreCleanupScopesQuery, map[string]any{"limit": limit})
	if err != nil {
		return nil, nil, err
	}
	workspaceUUIDSet := make(map[string]struct{})
	filesystemUUIDSet := make(map[string]struct{})
	cleanupScopeByNamespace := make(map[sessionResourceFileNamespaceKey]sessionResourceFileCleanupScope)
	var workspaceUUIDs []string
	var filesystemUUIDs []string
	for _, row := range scopeRows {
		workspaceUUID := row.WorkspaceUUID.String()
		filesystemUUID := row.FilesystemUUID.String()
		sessionUUID := row.SessionUUID.String()
		if _, found := workspaceUUIDSet[workspaceUUID]; !found {
			workspaceUUIDSet[workspaceUUID] = struct{}{}
			workspaceUUIDs = append(workspaceUUIDs, workspaceUUID)
		}
		if _, found := filesystemUUIDSet[filesystemUUID]; !found {
			filesystemUUIDSet[filesystemUUID] = struct{}{}
			filesystemUUIDs = append(filesystemUUIDs, filesystemUUID)
		}
		cleanupScopeByNamespace[sessionResourceFileNamespaceKey{
			WorkspaceUUID: workspaceUUID,
			SessionUUID:   sessionUUID,
		}] = sessionResourceFileCleanupScope{
			WorkspaceUUID: workspaceUUID, FilesystemUUID: filesystemUUID,
		}
	}
	if len(workspaceUUIDs) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}
	sort.Slice(workspaceUUIDs, func(i, j int) bool {
		return workspaceUUIDs[i] < workspaceUUIDs[j]
	})
	sort.Slice(filesystemUUIDs, func(i, j int) bool {
		return filesystemUUIDs[i] < filesystemUUIDs[j]
	})
	// 所有容量变更都先锁工作区，再锁文件系统；批处理内部也按 ID 升序取得同类锁。
	for _, workspaceUUID := range workspaceUUIDs {
		if _, err := namedExecContext(ctx, tx, `select pg_advisory_xact_lock(hashtextextended(CAST(:workspace_uuid AS text), 0))`, map[string]any{"workspace_uuid": workspaceUUID}); err != nil {
			return nil, nil, err
		}
	}
	for _, filesystemUUID := range filesystemUUIDs {
		if _, err := namedExecContext(ctx, tx, `select pg_advisory_xact_lock(hashtextextended(concat('filestore-filesystem', chr(58), CAST(:filesystem_uuid AS text)), 0))`, map[string]any{
			"filesystem_uuid": filesystemUUID,
		}); err != nil {
			return nil, nil, err
		}
	}

	var entryRows []sessionResourceFileRow
	err = namedSelectContext(ctx, tx, &entryRows, expiredSessionResourceFilesQuery, map[string]any{
		"filesystem_uuids": filesystemUUIDs,
		"limit":            limit,
	})
	if err != nil {
		return nil, nil, err
	}
	entries, err := sessionResourceFilesFromSQLXRows(entryRows)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	jobs := make([]FilestoreObjectCleanupJob, 0, len(entries))
	anomalies := make([]FilestoreCleanupAnomaly, 0)
	anomalyWorkspaces := make(map[string]struct{})
	releasedBytesByWorkspace := make(map[string]int64)
	for _, entry := range entries {
		// Input Resource 引用的 Source File 不由 Session namespace 拥有或计费。
		// schema 禁止这类引用设置 TTL；此守卫仍可避免异常历史数据触发对象清理。
		if entry.ReferencesSourceFile() {
			continue
		}
		scope, found := cleanupScopeByNamespace[sessionResourceFileNamespaceKey{
			WorkspaceUUID: entry.WorkspaceUUID,
			SessionUUID:   entry.SessionUUID,
		}]
		if !found {
			return nil, nil, ErrNotFound
		}
		if anomaly, malformed := sessionResourceFileCleanupAnomaly(scope, entry); malformed {
			anomalies = append(anomalies, anomaly)
			anomalyWorkspaces[scope.WorkspaceUUID] = struct{}{}
		} else {
			job, err := enqueueSessionResourceFileCleanupJobTx(ctx, tx, scope, entry, "ttl_expired", now)
			if err != nil {
				return nil, nil, err
			}
			jobs = append(jobs, job)
		}
		if err := retireSessionResourceFileTx(ctx, tx, scope.WorkspaceUUID, entry.UUID, now); err != nil {
			return nil, nil, err
		}
		releasedBytes, err := addWorkspaceStorageDelta(
			releasedBytesByWorkspace[scope.WorkspaceUUID], filestoreInt64(entry.SizeBytes),
		)
		if err != nil {
			return nil, nil, err
		}
		releasedBytesByWorkspace[scope.WorkspaceUUID] = releasedBytes
	}
	for _, workspaceUUID := range workspaceUUIDs {
		if _, malformed := anomalyWorkspaces[workspaceUUID]; malformed {
			if _, err := reconcileWorkspaceStorageUsageSQLXTx(ctx, tx, workspaceUUID); err != nil {
				return nil, nil, err
			}
			continue
		}
		releasedBytes := releasedBytesByWorkspace[workspaceUUID]
		if releasedBytes == 0 {
			continue
		}
		if err := applyWorkspaceStorageDeltaSQLXTx(ctx, tx, workspaceUUID, 0, -releasedBytes, 0); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return jobs, anomalies, nil
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
		filestoreFilesystemCleanupJobColumns("j", "fs"),
	)
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// ProcessLeasedFilestoreFilesystemCleanupJob 在一个短事务中退休有限数量的文件条目，
// 并把每个精确对象版本转换为既有对象清理任务。返回值表示整个文件系统是否已经退休完毕，
// anomalies 则交给 worker 记录无法定位对象的异常节点。
func (d *DB) ProcessLeasedFilestoreFilesystemCleanupJob(
	ctx context.Context,
	jobUUID string,
	leaseToken string,
	limit int,
) (bool, []FilestoreCleanupAnomaly, error) {
	if strings.TrimSpace(leaseToken) == "" {
		return false, nil, ErrPreconditionFailed
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback()

	arguments := map[string]any{
		"job_uuid":    jobUUID,
		"job_type":    filestoreFilesystemCleanupJobType,
		"lease_token": leaseToken,
		"limit":       limit,
	}
	var job FilestoreFilesystemCleanupJob
	err = namedGetContext(ctx, tx, &job, leasedFilesystemCleanupJobQuery, arguments)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, ErrVersionConflict
		}
		return false, nil, err
	}
	arguments["workspace_uuid"] = job.WorkspaceUUID
	arguments["filesystem_uuid"] = job.FilesystemUUID
	arguments["workspace_uuid"] = job.WorkspaceUUID
	arguments["filesystem_uuid"] = job.FilesystemUUID
	if _, err := namedExecContext(ctx, tx, filesystemCleanupWorkspaceLockQuery, arguments); err != nil {
		return false, nil, err
	}
	if _, err := namedExecContext(ctx, tx, filesystemCleanupFilesystemLockQuery, arguments); err != nil {
		return false, nil, err
	}

	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filesystemCleanupFilesystemQuery, arguments)
	if err != nil {
		return false, nil, err
	}
	arguments["session_uuid"] = filesystem.SessionUUID
	cleanupScope := sessionResourceFileCleanupScope{
		WorkspaceUUID: job.WorkspaceUUID, FilesystemUUID: filesystem.UUID,
	}

	var entryRows []sessionResourceFileRow
	err = namedSelectContext(ctx, tx, &entryRows, filesystemCleanupEntriesQuery, arguments)
	if err != nil {
		return false, nil, err
	}
	entries, err := sessionResourceFilesFromSQLXRows(entryRows)
	if err != nil {
		return false, nil, err
	}

	now := time.Now().UTC()
	arguments["retired_at"] = now
	anomalies := make([]FilestoreCleanupAnomaly, 0)
	var releasedBytes int64
	for _, entry := range entries {
		if !entry.ReferencesSourceFile() {
			if anomaly, malformed := sessionResourceFileCleanupAnomaly(cleanupScope, entry); malformed {
				anomalies = append(anomalies, anomaly)
			} else {
				if _, err := enqueueSessionResourceFileCleanupJobTx(ctx, tx, cleanupScope, entry, "session_deleted", now); err != nil {
					return false, nil, err
				}
				releasedBytes, err = addWorkspaceStorageDelta(
					releasedBytes,
					filestoreInt64(entry.SizeBytes),
				)
				if err != nil {
					return false, nil, err
				}
			}
		}
		if err := retireSessionResourceFileTx(ctx, tx, job.WorkspaceUUID, entry.UUID, now); err != nil {
			return false, nil, err
		}
	}
	if len(anomalies) > 0 {
		if _, err := reconcileWorkspaceStorageUsageSQLXTx(ctx, tx, job.WorkspaceUUID); err != nil {
			return false, nil, err
		}
	} else if releasedBytes > 0 {
		if err := applyWorkspaceStorageDeltaSQLXTx(ctx, tx, job.WorkspaceUUID, 0, -releasedBytes, 0); err != nil {
			return false, nil, err
		}
	}

	var filesRemain bool
	if err := namedGetContext(ctx, tx, &filesRemain, filesystemCleanupFilesRemainQuery, arguments); err != nil {
		return false, nil, err
	}
	if !filesRemain {
		if _, err := namedExecContext(ctx, tx, retireFilesystemCleanupSkillFilesQuery, arguments); err != nil {
			return false, nil, err
		}
		if _, err := namedExecContext(ctx, tx, retireFilesystemCleanupNamespaceEntriesQuery, arguments); err != nil {
			return false, nil, err
		}
	}

	status := "pending"
	if !filesRemain {
		status = "completed"
	}
	arguments["status"] = status
	rowsAffected, err := namedExecRowsAffected(ctx, tx, completeFilesystemCleanupBatchQuery, arguments)
	if err != nil {
		return false, nil, err
	}
	if rowsAffected == 0 {
		return false, nil, ErrVersionConflict
	}
	if err := tx.Commit(); err != nil {
		return false, nil, err
	}
	return !filesRemain, anomalies, nil
}

// FailLeasedFilestoreFilesystemCleanupJob 记录整文件系统清理失败并按统一退避策略重试。
func (d *DB) FailLeasedFilestoreFilesystemCleanupJob(ctx context.Context, jobUUID string, leaseToken, reason string, retryDelay time.Duration, maxAttempts int) error {
	return d.failLeasedFilestoreCleanupJob(
		ctx,
		jobUUID,
		leaseToken,
		reason,
		retryDelay,
		maxAttempts,
		filestoreFilesystemCleanupJobType,
	)
}

// EnqueueFilestoreObjectCleanupJob 持久化一条延迟对象删除任务。
func (d *DB) EnqueueFilestoreObjectCleanupJob(ctx context.Context, input EnqueueFilestoreObjectCleanupJobInput) (FilestoreObjectCleanupJob, error) {
	if input.WorkspaceUUID == "" || input.FilesystemUUID == "" ||
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
func (d *DB) AttachFilestoreObjectCleanupJobVersion(ctx context.Context, workspaceUUID string, jobExternalID, etag, versionID string) error {
	jobExternalID = strings.TrimSpace(jobExternalID)
	if workspaceUUID == "" || jobExternalID == "" {
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
				:workspace_uuid
			)
	`, map[string]any{
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
	return d.filestoreCleanupJobMutationMiss(ctx, workspaceUUID, jobExternalID)
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
		filestoreFilesystemCleanupJobColumns("j", "fs"),
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
func (d *DB) CompleteFilestoreObjectCleanupJob(ctx context.Context, jobUUID string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = 'completed', locked_by = null, locked_until = null, updated_at = now()
		where uuid = :job_uuid and type = :job_type and status in ('pending', 'retry')
	`, map[string]any{
		"job_uuid": jobUUID,
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
func (d *DB) CompleteLeasedFilestoreObjectCleanupJob(ctx context.Context, jobUUID string, leaseToken string) error {
	if strings.TrimSpace(leaseToken) == "" {
		return ErrPreconditionFailed
	}
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = 'completed', locked_by = null, locked_until = null, updated_at = now()
		where uuid = :job_uuid and type = :job_type and status = 'running'
			and locked_by = :lease_token and locked_until >= now()
	`, map[string]any{
		"job_uuid":    jobUUID,
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
func (d *DB) FailLeasedFilestoreObjectCleanupJob(ctx context.Context, jobUUID string, leaseToken, reason string, retryDelay time.Duration, maxAttempts int) error {
	return d.failLeasedFilestoreCleanupJob(
		ctx,
		jobUUID,
		leaseToken,
		reason,
		retryDelay,
		maxAttempts,
		filestoreCleanupJobType,
	)
}

func (d *DB) failLeasedFilestoreCleanupJob(ctx context.Context, jobUUID string, leaseToken, reason string, retryDelay time.Duration, maxAttempts int, jobType string) error {
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
		where uuid = :job_uuid and type = :job_type and status = 'running'
			and locked_by = :lease_token and locked_until >= now()
	`, map[string]any{
		"job_uuid":     jobUUID,
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

func enqueueFilestoreFilesystemCleanupJobTx(
	ctx context.Context,
	tx *sqlx.Tx,
	filesystem FilestoreFilesystem,
	workspaceUUID string,
	runAfter time.Time,
) (FilestoreFilesystemCleanupJob, error) {
	var job FilestoreFilesystemCleanupJob
	err := namedGetContext(ctx, tx, &job, enqueueFilestoreFilesystemCleanupJobQuery, map[string]any{
		"filesystem_uuid": filesystem.UUID,
		"job_type":        filestoreFilesystemCleanupJobType,
		"run_after":       runAfter,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreFilesystemCleanupJob{}, ErrNotFound
	}
	return job, err
}

// CancelFilestoreObjectCleanupJob 取消尚未被 worker 执行的清理任务。
func (d *DB) CancelFilestoreObjectCleanupJob(ctx context.Context, workspaceUUID string, jobExternalID string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, `
		update jobs
		set status = 'canceled', locked_by = null, locked_until = null, updated_at = now()
		where external_id = :job_external_id
			and type = :job_type
			and status in ('pending', 'retry')
			and payload->>'workspace_uuid' = (
				:workspace_uuid
			)
	`, map[string]any{
		"job_external_id": jobExternalID,
		"job_type":        filestoreCleanupJobType,
	})
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}
	return d.filestoreCleanupJobMutationMiss(ctx, workspaceUUID, jobExternalID)
}

func (d *DB) filestoreCleanupJobMutationMiss(ctx context.Context, workspaceUUID string, jobExternalID string) error {
	var status string
	err := namedGetContext(ctx, d.sql, &status, `
		select status
		from jobs
		where external_id = :job_external_id
			and type = :job_type
			and payload->>'workspace_uuid' = (
				:workspace_uuid
			)
	`, map[string]any{
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

type sessionResourceFileCleanupScope struct {
	WorkspaceUUID  string
	FilesystemUUID string
}

type sessionResourceFileNamespaceKey struct {
	WorkspaceUUID string
	SessionUUID   string
}

type expiredFilestoreCleanupScopeRow struct {
	WorkspaceUUID  uuid.UUID `db:"workspace_uuid"`
	FilesystemUUID uuid.UUID `db:"filesystem_uuid"`
	SessionUUID    uuid.UUID `db:"session_uuid"`
}

const filestoreCleanupAnomalyMissingObjectLocation = "missing_object_location"

func sessionResourceFileCleanupAnomaly(
	scope sessionResourceFileCleanupScope,
	entry SessionResourceFile,
) (FilestoreCleanupAnomaly, bool) {
	if entry.Kind != SessionResourceFileKindFile || entry.ReferencesSourceFile() ||
		entry.S3Bucket != nil && strings.TrimSpace(*entry.S3Bucket) != "" &&
			entry.S3Key != nil && strings.TrimSpace(*entry.S3Key) != "" {
		return FilestoreCleanupAnomaly{}, false
	}
	return FilestoreCleanupAnomaly{
		WorkspaceUUID:   scope.WorkspaceUUID,
		FilesystemUUID:  scope.FilesystemUUID,
		EntryExternalID: entry.ExternalID,
		Reason:          filestoreCleanupAnomalyMissingObjectLocation,
	}, true
}

func enqueueSessionResourceFileCleanupJobTx(ctx context.Context, tx *sqlx.Tx, scope sessionResourceFileCleanupScope, entry SessionResourceFile, reason string, runAfter time.Time) (FilestoreObjectCleanupJob, error) {
	// 该辅助函数也用于退役整个 filesystem。Owned File 必须进入对象清理；
	// Input Resource 只引用 Files API 对象，不能登记对象删除。
	if entry.Kind != SessionResourceFileKindFile || entry.S3Bucket == nil ||
		entry.S3Key == nil || entry.ReferencesSourceFile() {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	return insertFilestoreObjectCleanupJobSQLX(ctx, tx, EnqueueFilestoreObjectCleanupJobInput{
		WorkspaceUUID:   scope.WorkspaceUUID,
		FilesystemUUID:  scope.FilesystemUUID,
		EntryExternalID: entry.ExternalID,
		Bucket:          *entry.S3Bucket,
		Key:             *entry.S3Key,
		ETag:            filestoreString(entry.S3ETag),
		VersionID:       filestoreString(entry.S3VersionID),
		Reason:          reason,
		RunAfter:        runAfter,
	})
}

func enqueueOwnedSessionResourceFileCleanupJobTx(
	ctx context.Context,
	tx *sqlx.Tx,
	scope sessionResourceFileCleanupScope,
	entry SessionResourceFile,
	reason string,
	runAfter time.Time,
) (FilestoreObjectCleanupJob, bool, error) {
	if entry.ReferencesSourceFile() {
		return FilestoreObjectCleanupJob{}, false, nil
	}
	job, err := enqueueSessionResourceFileCleanupJobTx(ctx, tx, scope, entry, reason, runAfter)
	return job, err == nil, err
}

func enqueueFilestoreSubtreeCleanupJobsTx(ctx context.Context, tx *sqlx.Tx, scope sessionResourceFileCleanupScope, filesystem FilestoreFilesystem, rootPath string, runAfter time.Time) ([]FilestoreObjectCleanupJob, int64, error) {
	// rootPath 本身是目录，文件只可能出现在严格后代中；分隔符比较避免同前缀误选。
	var rows []sessionResourceFileRow
	err := namedSelectContext(ctx, tx, &rows, sessionResourceFileSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and kind = 'file'
			and deleted_at is null
			and left(path, char_length(:root_path) + 1) = :root_path || '/'
		order by id
	`, filestoreSubtreeArguments(filesystem, rootPath))
	if err != nil {
		return nil, 0, err
	}
	entries, err := sessionResourceFilesFromSQLXRows(rows)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]FilestoreObjectCleanupJob, 0, len(entries))
	var removedBytes int64
	for _, entry := range entries {
		job, enqueued, err := enqueueOwnedSessionResourceFileCleanupJobTx(ctx, tx, scope, entry, "remove_directory", runAfter)
		if err != nil {
			return nil, 0, err
		}
		if enqueued {
			jobs = append(jobs, job)
		}
		removedBytes, err = addWorkspaceStorageDelta(removedBytes, entry.OwnedBytes())
		if err != nil {
			return nil, 0, err
		}
	}
	return jobs, removedBytes, nil
}

func retireExpiredFilestoreSubtreeTx(
	ctx context.Context,
	tx *sqlx.Tx,
	scope sessionResourceFileCleanupScope,
	filesystem FilestoreFilesystem,
	rootPath string,
	retiredAt time.Time,
) ([]FilestoreObjectCleanupJob, int64, error) {
	var rows []sessionResourceFileRow
	err := namedSelectContext(ctx, tx, &rows, sessionResourceFileSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and kind = 'file'
			and deleted_at is null and expires_at <= now()
			and (
				path = :root_path
				or left(path, char_length(:root_path) + 1) = :root_path || '/'
			)
		order by id
	`, filestoreSubtreeArguments(filesystem, rootPath))
	if err != nil {
		return nil, 0, err
	}
	entries, err := sessionResourceFilesFromSQLXRows(rows)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]FilestoreObjectCleanupJob, 0, len(entries))
	var retiredBytes int64
	for _, entry := range entries {
		job, enqueued, err := enqueueOwnedSessionResourceFileCleanupJobTx(ctx, tx, scope, entry, "expired_destination_replaced", retiredAt)
		if err != nil {
			return nil, 0, err
		}
		if enqueued {
			jobs = append(jobs, job)
		}
		retiredBytes, err = addWorkspaceStorageDelta(retiredBytes, entry.OwnedBytes())
		if err != nil {
			return nil, 0, err
		}
		if err := retireSessionResourceFileTx(ctx, tx, scope.WorkspaceUUID, entry.UUID, retiredAt); err != nil {
			return nil, 0, err
		}
	}
	return jobs, retiredBytes, nil
}

func filestoreSubtreeArguments(filesystem FilestoreFilesystem, rootPath string) map[string]any {
	return map[string]any{
		"workspace_uuid": filesystem.WorkspaceUUID,
		"session_uuid":   filesystem.SessionUUID,
		"root_path":      rootPath,
	}
}

func cancelAttachedFilestoreObjectCleanupJobTx(ctx context.Context, tx *sqlx.Tx, workspaceUUID string, jobExternalID string, blob FilestoreFileBlob) error {
	// 将哨兵取消与文件条目提交置于同一事务；任一失败都会保留可重试的清理路径。
	rowsAffected, err := namedExecRowsAffected(ctx, tx, `
		update jobs
		set status = 'canceled', locked_by = null, locked_until = null, updated_at = now()
		where external_id = :job_external_id and type = :job_type
			and status in ('pending', 'retry')
			and payload->>'workspace_uuid' = (
				:workspace_uuid
			)
			and payload->>'bucket' = :bucket
			and payload->>'key' = :key
			and coalesce(payload->>'version_id', '') = :version_id
	`, map[string]any{
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

func filestoreFilesystemCleanupJobColumns(jobAlias, filesystemAlias string) string {
	return fmt.Sprintf(`cast(%[1]s.uuid as text) as uuid, %[1]s.external_id as external_id,
		cast(%[1]s.workspace_uuid as text) as workspace_uuid,
		cast(%[2]s.uuid as text) as filesystem_uuid,
		%[2]s.external_id as filesystem_external_id,
		coalesce(%[1]s.payload->>'entry_external_id', '') as entry_external_id,
		coalesce(%[1]s.payload->>'bucket', '') as bucket,
		coalesce(%[1]s.payload->>'key', '') as key,
		coalesce(%[1]s.payload->>'etag', '') as etag,
		coalesce(%[1]s.payload->>'version_id', '') as version_id,
		coalesce(%[1]s.payload->>'reason', '') as reason,
		%[1]s.attempts as attempts, %[1]s.run_after as run_after`,
		jobAlias, filesystemAlias)
}
