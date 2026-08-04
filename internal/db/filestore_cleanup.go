package db

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/yourbatis"
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
	var jobs []FilestoreObjectCleanupJob
	var anomalies []FilestoreCleanupAnomaly
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		var transactionErr error
		jobs, anomalies, transactionErr = expireSessionResourceFilesTx(ctx, executor, limit)
		return transactionErr
	})
	if err != nil {
		return nil, nil, err
	}
	return jobs, anomalies, nil
}

func expireSessionResourceFilesTx(
	ctx context.Context,
	tx yourbatis.Executor,
	limit int,
) ([]FilestoreObjectCleanupJob, []FilestoreCleanupAnomaly, error) {
	cleanupMapper := NewFilestoreCleanupMapper(tx)
	scopeRows, err := cleanupMapper.ListExpiredScopes(ctx, limit)
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
		return nil, nil, nil
	}
	sort.Slice(workspaceUUIDs, func(i, j int) bool {
		return workspaceUUIDs[i] < workspaceUUIDs[j]
	})
	sort.Slice(filesystemUUIDs, func(i, j int) bool {
		return filesystemUUIDs[i] < filesystemUUIDs[j]
	})
	// 所有容量变更都先锁工作区，再锁文件系统；批处理内部也按 ID 升序取得同类锁。
	storageMapper := NewWorkspaceStorageUsageMapper(tx)
	for _, workspaceUUID := range workspaceUUIDs {
		if err := storageMapper.LockWorkspace(ctx, workspaceUUID); err != nil {
			return nil, nil, err
		}
	}
	filesystemMapper := NewFilestoreFilesystemMapper(tx)
	for _, filesystemUUID := range filesystemUUIDs {
		if err := filesystemMapper.LockFilesystem(ctx, filesystemUUID); err != nil {
			return nil, nil, err
		}
	}

	entryRows, err := cleanupMapper.ListExpiredFiles(ctx, filestoreExpiredFilesMapperParams{
		FilesystemUUIDs: filesystemUUIDs,
		Limit:           limit,
	})
	if err != nil {
		return nil, nil, err
	}
	entries, err := sessionResourceFilesFromMapperRows(entryRows)
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
			if _, err := reconcileWorkspaceStorageUsageTx(ctx, tx, workspaceUUID); err != nil {
				return nil, nil, err
			}
			continue
		}
		releasedBytes := releasedBytesByWorkspace[workspaceUUID]
		if releasedBytes == 0 {
			continue
		}
		if err := applyWorkspaceStorageDeltaTx(ctx, tx, workspaceUUID, 0, -releasedBytes, 0); err != nil {
			return nil, nil, err
		}
	}
	return jobs, anomalies, nil
}

// LeaseFilestoreFilesystemCleanupJobs 租约一批待拆分的整文件系统清理任务。
func (d *DB) LeaseFilestoreFilesystemCleanupJobs(ctx context.Context, workerID string, limit, maxLeaseAttempts int) ([]FilestoreFilesystemCleanupJob, error) {
	params := normalizeFilestoreCleanupJobLeaseParams(
		filestoreFilesystemCleanupJobType,
		workerID,
		limit,
		maxLeaseAttempts,
	)
	rows, err := NewFilestoreCleanupMapper(d.mapperDB).LeaseFilesystemJobs(ctx, params)
	if err != nil {
		return nil, err
	}
	return filestoreFilesystemCleanupJobsFromMapperRows(rows), nil
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
	var completed bool
	var anomalies []FilestoreCleanupAnomaly
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		var transactionErr error
		completed, anomalies, transactionErr = processLeasedFilestoreFilesystemCleanupJobTx(
			ctx,
			executor,
			jobUUID,
			leaseToken,
			limit,
		)
		return transactionErr
	})
	if err != nil {
		return false, nil, err
	}
	return completed, anomalies, nil
}

func processLeasedFilestoreFilesystemCleanupJobTx(
	ctx context.Context,
	tx yourbatis.Executor,
	jobUUID string,
	leaseToken string,
	limit int,
) (bool, []FilestoreCleanupAnomaly, error) {
	cleanupMapper := NewFilestoreCleanupMapper(tx)
	leaseIdentity := filestoreCleanupJobLeaseIdentity{
		JobUUID:    jobUUID,
		JobType:    filestoreFilesystemCleanupJobType,
		LeaseToken: leaseToken,
	}
	jobRow, err := cleanupMapper.GetLeasedFilesystemJob(ctx, leaseIdentity)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, ErrVersionConflict
		}
		return false, nil, err
	}
	job := jobRow.job()
	if err := NewWorkspaceStorageUsageMapper(tx).LockWorkspace(ctx, job.WorkspaceUUID); err != nil {
		return false, nil, err
	}
	if err := NewFilestoreFilesystemMapper(tx).LockFilesystem(ctx, job.FilesystemUUID); err != nil {
		return false, nil, err
	}

	filesystemRow, err := cleanupMapper.GetFilesystemForCleanup(
		ctx,
		job.WorkspaceUUID,
		job.FilesystemUUID,
	)
	if err != nil {
		return false, nil, err
	}
	filesystem, err := filesystemRow.filesystem()
	if err != nil {
		return false, nil, err
	}
	cleanupScope := sessionResourceFileCleanupScope{
		WorkspaceUUID: job.WorkspaceUUID, FilesystemUUID: filesystem.UUID,
	}

	batchParams := filestoreFilesystemBatchMapperParams{
		JobUUID:        jobUUID,
		JobType:        filestoreFilesystemCleanupJobType,
		LeaseToken:     leaseToken,
		WorkspaceUUID:  job.WorkspaceUUID,
		FilesystemUUID: job.FilesystemUUID,
		SessionUUID:    filesystem.SessionUUID,
		Limit:          limit,
	}
	entryRows, err := cleanupMapper.ListFilesystemFiles(ctx, batchParams)
	if err != nil {
		return false, nil, err
	}
	entries, err := sessionResourceFilesFromMapperRows(entryRows)
	if err != nil {
		return false, nil, err
	}

	now := time.Now().UTC()
	batchParams.RetiredAt = now
	anomalies, releasedBytes, err := processFilesystemCleanupEntries(
		ctx, tx, cleanupScope, job.WorkspaceUUID, entries, now,
	)
	if err != nil {
		return false, nil, err
	}
	if len(anomalies) > 0 {
		if _, err := reconcileWorkspaceStorageUsageTx(ctx, tx, job.WorkspaceUUID); err != nil {
			return false, nil, err
		}
	} else if releasedBytes > 0 {
		if err := applyWorkspaceStorageDeltaTx(ctx, tx, job.WorkspaceUUID, 0, -releasedBytes, 0); err != nil {
			return false, nil, err
		}
	}

	filesRemain, err := cleanupMapper.FilesystemFilesRemain(
		ctx,
		batchParams.WorkspaceUUID,
		batchParams.SessionUUID,
	)
	if err != nil {
		return false, nil, err
	}
	if !filesRemain {
		if err := cleanupMapper.RetireSkillFiles(ctx, batchParams); err != nil {
			return false, nil, err
		}
		if err := cleanupMapper.RetireNamespace(ctx, batchParams); err != nil {
			return false, nil, err
		}
	}

	status := "pending"
	if !filesRemain {
		status = "completed"
	}
	batchParams.Status = status
	rowsAffected, err := cleanupMapper.CompleteFilesystemBatch(ctx, batchParams)
	if err != nil {
		return false, nil, err
	}
	if rowsAffected == 0 {
		return false, nil, ErrVersionConflict
	}
	return !filesRemain, anomalies, nil
}

func processFilesystemCleanupEntries(
	ctx context.Context,
	tx yourbatis.Executor,
	cleanupScope sessionResourceFileCleanupScope,
	workspaceUUID string,
	entries []SessionResourceFile,
	retiredAt time.Time,
) ([]FilestoreCleanupAnomaly, int64, error) {
	anomalies := make([]FilestoreCleanupAnomaly, 0)
	var releasedBytes int64
	for _, entry := range entries {
		if !entry.ReferencesSourceFile() {
			if anomaly, malformed := sessionResourceFileCleanupAnomaly(cleanupScope, entry); malformed {
				anomalies = append(anomalies, anomaly)
			} else {
				if _, err := enqueueSessionResourceFileCleanupJobTx(
					ctx, tx, cleanupScope, entry, "session_deleted", retiredAt,
				); err != nil {
					return nil, 0, err
				}
				var err error
				releasedBytes, err = addWorkspaceStorageDelta(
					releasedBytes,
					filestoreInt64(entry.SizeBytes),
				)
				if err != nil {
					return nil, 0, err
				}
			}
		}
		if err := retireSessionResourceFileTx(ctx, tx, workspaceUUID, entry.UUID, retiredAt); err != nil {
			return nil, 0, err
		}
	}
	return anomalies, releasedBytes, nil
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
	return insertFilestoreObjectCleanupJob(ctx, d.mapperDB, input)
}

func insertFilestoreObjectCleanupJob(
	ctx context.Context,
	database yourbatis.Executor,
	input EnqueueFilestoreObjectCleanupJobInput,
) (FilestoreObjectCleanupJob, error) {
	jobRow, err := NewFilestoreCleanupMapper(database).InsertObjectJob(
		ctx,
		filestoreCleanupJobInsertParams{
			WorkspaceUUID:   input.WorkspaceUUID,
			JobType:         filestoreCleanupJobType,
			FilesystemUUID:  input.FilesystemUUID,
			EntryExternalID: input.EntryExternalID,
			Bucket:          input.Bucket,
			Key:             input.Key,
			ETag:            input.ETag,
			VersionID:       input.VersionID,
			Reason:          input.Reason,
			RunAfter:        input.RunAfter,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	if err != nil {
		return FilestoreObjectCleanupJob{}, err
	}
	return jobRow.job(), nil
}

// AttachFilestoreObjectCleanupJobVersion 在文件元数据提交前记录刚上传对象的精确版本。
// 若进程随后崩溃，遗留任务仍能删除该版本，而不是在版本化桶中仅新增一个删除标记。
func (d *DB) AttachFilestoreObjectCleanupJobVersion(ctx context.Context, workspaceUUID string, jobExternalID, etag, versionID string) error {
	jobExternalID = strings.TrimSpace(jobExternalID)
	if workspaceUUID == "" || jobExternalID == "" {
		return ErrPreconditionFailed
	}
	rowsAffected, err := NewFilestoreCleanupMapper(d.mapperDB).AttachObjectVersion(
		ctx,
		filestoreCleanupJobMutationParams{
			JobExternalID: jobExternalID,
			WorkspaceUUID: workspaceUUID,
			ETag:          etag,
			VersionID:     versionID,
			JobType:       filestoreCleanupJobType,
		},
	)
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
	params := normalizeFilestoreCleanupJobLeaseParams(
		filestoreCleanupJobType,
		workerID,
		limit,
		maxLeaseAttempts,
	)
	rows, err := NewFilestoreCleanupMapper(d.mapperDB).LeaseObjectJobs(ctx, params)
	if err != nil {
		return nil, err
	}
	return filestoreObjectCleanupJobsFromMapperRows(rows), nil
}

func normalizeFilestoreCleanupJobLeaseParams(
	jobType string,
	workerID string,
	limit int,
	maxLeaseAttempts int,
) filestoreCleanupJobLeaseParams {
	if limit <= 0 {
		limit = 10
	}
	if maxLeaseAttempts <= 0 {
		maxLeaseAttempts = 10
	}
	return filestoreCleanupJobLeaseParams{
		JobType:          jobType,
		WorkerID:         workerID,
		Limit:            limit,
		MaxLeaseAttempts: maxLeaseAttempts,
	}
}

// CompleteFilestoreObjectCleanupJob 完成一条尚未出租的任务，供请求内即时补偿使用。
func (d *DB) CompleteFilestoreObjectCleanupJob(ctx context.Context, jobUUID string) error {
	rowsAffected, err := NewFilestoreCleanupMapper(d.mapperDB).CompletePendingObjectJob(
		ctx,
		filestoreCleanupJobMutationParams{
			JobUUID: jobUUID,
			JobType: filestoreCleanupJobType,
		},
	)
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
	rowsAffected, err := NewFilestoreCleanupMapper(d.mapperDB).CompleteLeasedObjectJob(
		ctx,
		filestoreCleanupJobMutationParams{
			JobUUID:    jobUUID,
			JobType:    filestoreCleanupJobType,
			LeaseToken: leaseToken,
		},
	)
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
	rowsAffected, err := NewFilestoreCleanupMapper(d.mapperDB).FailLeasedJob(
		ctx,
		filestoreCleanupJobMutationParams{
			JobUUID:     jobUUID,
			Reason:      reason,
			RunAfter:    runAfter,
			MaxAttempts: maxAttempts,
			JobType:     jobType,
			LeaseToken:  leaseToken,
		},
	)
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
	tx yourbatis.Executor,
	filesystem FilestoreFilesystem,
	workspaceUUID string,
	runAfter time.Time,
) (FilestoreFilesystemCleanupJob, error) {
	jobRow, err := NewFilestoreCleanupMapper(tx).InsertFilesystemJob(
		ctx,
		filestoreCleanupJobInsertParams{
			WorkspaceUUID:  workspaceUUID,
			FilesystemUUID: filesystem.UUID,
			JobType:        filestoreFilesystemCleanupJobType,
			RunAfter:       runAfter,
		},
	)
	if errors.Is(err, sql.ErrNoRows) {
		return FilestoreFilesystemCleanupJob{}, ErrNotFound
	}
	return jobRow.job(), err
}

// CancelFilestoreObjectCleanupJob 取消尚未被 worker 执行的清理任务。
func (d *DB) CancelFilestoreObjectCleanupJob(ctx context.Context, workspaceUUID string, jobExternalID string) error {
	rowsAffected, err := NewFilestoreCleanupMapper(d.mapperDB).CancelPendingObjectJob(
		ctx,
		filestoreCleanupJobMutationParams{
			JobExternalID: jobExternalID,
			WorkspaceUUID: workspaceUUID,
			JobType:       filestoreCleanupJobType,
		},
	)
	if err != nil {
		return err
	}
	if rowsAffected > 0 {
		return nil
	}
	return d.filestoreCleanupJobMutationMiss(ctx, workspaceUUID, jobExternalID)
}

func (d *DB) filestoreCleanupJobMutationMiss(ctx context.Context, workspaceUUID string, jobExternalID string) error {
	_, err := NewFilestoreCleanupMapper(d.mapperDB).GetObjectJobStatus(
		ctx,
		filestoreCleanupJobMutationParams{
			JobExternalID: jobExternalID,
			WorkspaceUUID: workspaceUUID,
			JobType:       filestoreCleanupJobType,
		},
	)
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

func enqueueSessionResourceFileCleanupJobTx(ctx context.Context, tx yourbatis.Executor, scope sessionResourceFileCleanupScope, entry SessionResourceFile, reason string, runAfter time.Time) (FilestoreObjectCleanupJob, error) {
	// 该辅助函数也用于退役整个 filesystem。Owned File 必须进入对象清理；
	// Input Resource 只引用 Files API 对象，不能登记对象删除。
	if entry.Kind != SessionResourceFileKindFile || entry.S3Bucket == nil ||
		entry.S3Key == nil || entry.ReferencesSourceFile() {
		return FilestoreObjectCleanupJob{}, ErrPreconditionFailed
	}
	return insertFilestoreObjectCleanupJob(ctx, tx, EnqueueFilestoreObjectCleanupJobInput{
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
	tx yourbatis.Executor,
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

func enqueueFilestoreSubtreeCleanupJobsTx(ctx context.Context, tx yourbatis.Executor, scope sessionResourceFileCleanupScope, filesystem FilestoreFilesystem, rootPath string, runAfter time.Time) ([]FilestoreObjectCleanupJob, int64, error) {
	// rootPath 本身是目录，文件只可能出现在严格后代中；分隔符比较避免同前缀误选。
	rows, err := NewFilestoreCleanupMapper(tx).ListSubtreeFiles(
		ctx,
		filestoreSubtreeMapperParameters(filesystem, rootPath),
	)
	if err != nil {
		return nil, 0, err
	}
	entries, err := sessionResourceFilesFromMapperRows(rows)
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
	tx yourbatis.Executor,
	scope sessionResourceFileCleanupScope,
	filesystem FilestoreFilesystem,
	rootPath string,
	retiredAt time.Time,
) ([]FilestoreObjectCleanupJob, int64, error) {
	rows, err := NewFilestoreCleanupMapper(tx).ListExpiredSubtreeFiles(
		ctx,
		filestoreSubtreeMapperParameters(filesystem, rootPath),
	)
	if err != nil {
		return nil, 0, err
	}
	entries, err := sessionResourceFilesFromMapperRows(rows)
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

func filestoreSubtreeMapperParameters(filesystem FilestoreFilesystem, rootPath string) filestoreSubtreeMapperParams {
	return filestoreSubtreeMapperParams{
		WorkspaceUUID: filesystem.WorkspaceUUID,
		SessionUUID:   filesystem.SessionUUID,
		RootPath:      rootPath,
	}
}

func cancelAttachedFilestoreObjectCleanupJobTx(ctx context.Context, tx yourbatis.Executor, workspaceUUID string, jobExternalID string, blob FilestoreFileBlob) error {
	// 将哨兵取消与文件条目提交置于同一事务；任一失败都会保留可重试的清理路径。
	rowsAffected, err := NewFilestoreCleanupMapper(tx).CancelAttachedObjectJob(
		ctx,
		filestoreCleanupJobMutationParams{
			JobExternalID: jobExternalID,
			WorkspaceUUID: workspaceUUID,
			JobType:       filestoreCleanupJobType,
			Bucket:        blob.S3Bucket,
			Key:           blob.S3Key,
			VersionID:     blob.S3VersionID,
		},
	)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrFilestoreCleanupJobNotCancelable
	}
	return nil
}
