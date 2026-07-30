package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	fileSQLXColumns = `cast(uuid as text) as uuid, external_id, cast(workspace_uuid as text) as workspace_uuid, filename, mime_type,
		size_bytes, sha256, s3_bucket, s3_key, downloadable, scope_type, scope_id,
		cast(created_by_api_key_uuid as text) as created_by_api_key_uuid, created_at`
	getFileQuery = `
		select ` + fileSQLXColumns + `
		from files
		where workspace_uuid = cast(:workspace_uuid as uuid)
			and external_id = :file_external_id
			and deleted_at is null
	`
	getFileByUUIDQuery = `
		select ` + fileSQLXColumns + `
		from files
		where workspace_uuid = cast(:workspace_uuid as uuid)
			and cast(uuid as text) = :file_uuid
			and deleted_at is null
	`
	getFileByUUIDInOrganizationQuery = `
		select cast(f.uuid as text) as uuid, f.external_id, cast(f.workspace_uuid as text) as workspace_uuid,
			f.filename, f.mime_type, f.size_bytes, f.sha256, f.s3_bucket, f.s3_key,
			f.downloadable, f.scope_type, f.scope_id,
			cast(f.created_by_api_key_uuid as text) as created_by_api_key_uuid, f.created_at
		from files f
		join workspaces w on w.uuid = f.workspace_uuid
		where w.organization_uuid = cast(:organization_uuid as uuid)
			and cast(f.uuid as text) = :file_uuid
			and f.deleted_at is null
	`
	insertFileQuery = `
		insert into files (
			uuid, external_id, workspace_uuid, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_uuid, created_at
		)
		values (
			:file_uuid, :file_external_id, :workspace_uuid, :filename, :mime_type,
			:size_bytes, :sha256, :s3_bucket, :s3_key, :downloadable, :scope_type,
			:scope_id, :created_by_api_key_uuid, :created_at
		)
	`
	fileWorkspaceLockQuery    = `select pg_advisory_xact_lock(hashtextextended(cast(:workspace_uuid as text), 0))`
	softDeleteFileRecordQuery = `
		select ` + fileSQLXColumns + `
		from files
		where workspace_uuid = cast(:workspace_uuid as uuid)
			and external_id = :file_external_id
			and deleted_at is null
		for update
	`
	activeFileReferenceQuery = `
		select
			exists (
				select 1
				from filestore_entries entry
				where entry.workspace_uuid = cast(:workspace_uuid as uuid)
					-- 投影不拥有对象也不计入 files_bytes。即使 backing entry 已退休，
					-- 仍需阻止普通 Files 删除路径把投影当作源文件扣减容量。
					and entry.uuid = CAST(:file_uuid AS uuid)
			)
			or exists (
				select 1
				from filestore_entries entry
				join filestore_filesystems filesystem
					on filesystem.uuid = entry.filesystem_uuid
					and filesystem.workspace_uuid = entry.workspace_uuid
					and filesystem.deleted_at is null
				where entry.workspace_uuid = cast(:workspace_uuid as uuid)
					and entry.source_file_uuid = CAST(:file_uuid AS uuid)
					and entry.deleted_at is null
			)
	`
	softDeleteFileQuery = `
		update files
		set deleted_at = now()
		where workspace_uuid = cast(:workspace_uuid as uuid)
			and external_id = :file_external_id
			and deleted_at is null
	`
	enqueueObjectCleanupResourceJobQuery = `
		insert into jobs (external_id, workspace_uuid, type, status, payload)
		values (
			concat('job_', replace(cast(gen_random_uuid() as text), '-', '')),
			:workspace_uuid,
			'object_cleanup',
			'pending',
			jsonb_build_object(
				'bucket', CAST(:bucket AS text),
				'key', CAST(:object_key AS text),
				'file_id', case
					when CAST(:resource_type AS text) = 'file'
					then CAST(:resource_id AS text)
					else ''
				end,
				'resource_type', CAST(:resource_type AS text),
				'resource_id', CAST(:resource_id AS text)
			)
		)
	`
	leaseObjectCleanupJobsQuery = `
		with next_jobs as (
			select uuid
			from jobs
			where type = 'object_cleanup'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit :limit
			for update skip locked
		)
		update jobs j
		set status = 'running',
			locked_by = :worker_id,
			locked_until = now() + interval '1 minute',
			updated_at = now()
		from next_jobs
		where j.uuid = next_jobs.uuid
		returning cast(j.uuid as text) as uuid, j.external_id,
			cast(j.workspace_uuid as text) as workspace_uuid,
			coalesce(j.payload->>'bucket', '') as bucket,
			coalesce(j.payload->>'key', '') as object_key,
			coalesce(j.payload->>'file_id', '') as file_external_id,
			j.attempts
	`
	completeObjectCleanupJobQuery = `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where uuid = cast(:job_uuid as uuid) and type = 'object_cleanup'
	`
	failObjectCleanupJobQuery = `
		update jobs
		set status = :status,
			locked_by = null,
			locked_until = null,
			run_after = :run_after,
			updated_at = now(),
			attempts = :attempts,
			payload = payload || jsonb_build_object(
				'last_error',
				CAST(:reason AS text)
			)
		where uuid = cast(:job_uuid as uuid) and type = 'object_cleanup'
	`
)

// fileRecordRow / objectCleanupJobRow 用于承接 sqlx 的结构化扫描结果，避免把
// 数据库存储细节直接泄漏到上层文件服务。
type fileRecordRow struct {
	UUID                string    `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	WorkspaceUUID       string    `db:"workspace_uuid"`
	Filename            string    `db:"filename"`
	MimeType            string    `db:"mime_type"`
	SizeBytes           int64     `db:"size_bytes"`
	SHA256              string    `db:"sha256"`
	S3Bucket            string    `db:"s3_bucket"`
	S3Key               string    `db:"s3_key"`
	Downloadable        bool      `db:"downloadable"`
	ScopeType           *string   `db:"scope_type"`
	ScopeID             *string   `db:"scope_id"`
	CreatedByAPIKeyUUID string    `db:"created_by_api_key_uuid"`
	CreatedAt           time.Time `db:"created_at"`
}

type filePageCursorRow struct {
	UUID      string    `db:"uuid"`
	CreatedAt time.Time `db:"created_at"`
}

type objectCleanupJobRow struct {
	UUID           string `db:"uuid"`
	ExternalID     string `db:"external_id"`
	WorkspaceUUID  string `db:"workspace_uuid"`
	Bucket         string `db:"bucket"`
	Key            string `db:"object_key"`
	FileExternalID string `db:"file_external_id"`
	Attempts       int    `db:"attempts"`
}

func getFileArguments(workspaceUUID string, fileExternalID string) map[string]any {
	return map[string]any{
		"workspace_uuid":   workspaceUUID,
		"file_external_id": fileExternalID,
	}
}

func fileUUIDArguments(workspaceUUID string, fileUUID string) map[string]any {
	return map[string]any{
		"workspace_uuid": workspaceUUID,
		"file_uuid":      fileUUID,
	}
}

func fileRecordArguments(file FileRecord) map[string]any {
	return map[string]any{
		"file_uuid":               file.UUID,
		"file_external_id":        file.ExternalID,
		"workspace_uuid":          file.WorkspaceUUID,
		"filename":                file.Filename,
		"mime_type":               file.MimeType,
		"size_bytes":              file.SizeBytes,
		"sha256":                  file.SHA256,
		"s3_bucket":               file.S3Bucket,
		"s3_key":                  file.S3Key,
		"downloadable":            file.Downloadable,
		"scope_type":              file.ScopeType,
		"scope_id":                file.ScopeID,
		"created_by_api_key_uuid": file.CreatedByAPIKeyUUID,
		"created_at":              file.CreatedAt,
	}
}

// createFileSQLX 在同一个 sqlx 事务里写入文件记录并同步增加 workspace
// 存储用量，保证元数据与配额统计一致。
func createFileSQLX(
	ctx context.Context,
	database *sqlx.DB,
	file FileRecord,
) error {
	tx, err := beginFileCreateSQLXTx(ctx, database, file.WorkspaceUUID)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertFileSQLXTx(ctx, tx, file); err != nil {
		return err
	}
	if err := applyWorkspaceStorageDeltaSQLXTx(
		ctx,
		tx,
		file.WorkspaceUUID,
		file.SizeBytes,
		0,
		0,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// createFileIfWithinLimitSQLX 先尝试预扣本次写入会消耗的存储额度，只有在
// workspace 仍未超限时才真正插入文件记录。
func createFileIfWithinLimitSQLX(
	ctx context.Context,
	database *sqlx.DB,
	file FileRecord,
	workspaceStorageLimitBytes int64,
) error {
	tx, err := beginFileCreateSQLXTx(ctx, database, file.WorkspaceUUID)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := applyWorkspaceStorageDeltaSQLXTx(
		ctx,
		tx,
		file.WorkspaceUUID,
		file.SizeBytes,
		0,
		workspaceStorageLimitBytes,
	); err != nil {
		return err
	}
	if err := insertFileSQLXTx(ctx, tx, file); err != nil {
		return err
	}
	return tx.Commit()
}

func beginFileCreateSQLXTx(ctx context.Context, database *sqlx.DB, workspaceUUID string) (*sqlx.Tx, error) {
	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := namedExecContext(
		ctx,
		tx,
		fileWorkspaceLockQuery,
		map[string]any{"workspace_uuid": workspaceUUID},
	); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func insertFileSQLXTx(ctx context.Context, tx *sqlx.Tx, file FileRecord) error {
	_, err := namedExecContext(ctx, tx, insertFileQuery, fileRecordArguments(file))
	return err
}

func getFileRecordSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (FileRecord, error) {
	var row fileRecordRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FileRecord{}, ErrNotFound
		}
		return FileRecord{}, err
	}
	return row.record(), nil
}

func listFilesSQLXQuery(workspaceUUID string, scopeID string) (string, map[string]any) {
	query := `
		select ` + fileSQLXColumns + `
		from files
		where workspace_uuid = cast(:workspace_uuid as uuid)
			and deleted_at is null
	`
	arguments := map[string]any{"workspace_uuid": workspaceUUID}
	if scopeID != "" {
		query += " and scope_id = :scope_id"
		arguments["scope_id"] = scopeID
	}
	query += " order by created_at desc, uuid desc"
	return query, arguments
}

func listFileRecordsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]FileRecord, error) {
	var rows []fileRecordRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, nil
	}
	files := make([]FileRecord, 0, len(rows))
	for _, row := range rows {
		files = append(files, row.record())
	}
	return files, nil
}

func listFilesPageSQLX(
	ctx context.Context,
	database *sqlx.DB,
	params ListFilesPageParams,
) ([]FileRecord, bool, error) {
	params = normalizeListFilesPageParams(params)
	cursor, found, err := getFilePageCursorSQLX(ctx, database, params)
	if err != nil || !found {
		return nil, false, err
	}
	query, arguments := listFilesPageSQLXQuery(params, cursor)
	files, err := listFileRecordsSQLX(ctx, database, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(files) > params.Limit
	if hasMore {
		files = files[:params.Limit]
	}
	if params.BeforeID != "" {
		slices.Reverse(files)
	}
	return files, hasMore, nil
}

func normalizeListFilesPageParams(params ListFilesPageParams) ListFilesPageParams {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.AfterID != "" {
		params.BeforeID = ""
	}
	return params
}

func getFilePageCursorSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	params ListFilesPageParams,
) (filePageCursorRow, bool, error) {
	cursorExternalID := params.AfterID
	if cursorExternalID == "" {
		cursorExternalID = params.BeforeID
	}
	if cursorExternalID == "" {
		return filePageCursorRow{}, true, nil
	}
	query, arguments := filePageCursorSQLXQuery(params, cursorExternalID)
	var cursor filePageCursorRow
	if err := namedGetContext(ctx, database, &cursor, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return filePageCursorRow{}, false, nil
		}
		return filePageCursorRow{}, false, err
	}
	return cursor, true, nil
}

func filePageCursorSQLXQuery(
	params ListFilesPageParams,
	cursorExternalID string,
) (string, map[string]any) {
	query := `
		select cast(uuid as text) as uuid, created_at
		from files
		where workspace_uuid = cast(:workspace_uuid as uuid)
			and external_id = :cursor_external_id
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid":     params.WorkspaceUUID,
		"cursor_external_id": cursorExternalID,
	}
	if params.ScopeID != "" {
		query += " and scope_id = :scope_id"
		arguments["scope_id"] = params.ScopeID
	}
	return query, arguments
}

func listFilesPageSQLXQuery(
	params ListFilesPageParams,
	cursor filePageCursorRow,
) (string, map[string]any) {
	query := `
		select ` + fileSQLXColumns + `
		from files
		where workspace_uuid = cast(:workspace_uuid as uuid)
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": params.WorkspaceUUID,
		"limit":          params.Limit + 1,
	}
	if params.ScopeID != "" {
		query += " and scope_id = :scope_id"
		arguments["scope_id"] = params.ScopeID
	}
	if params.AfterID != "" {
		query += `
			and (
				created_at < :cursor_created_at
				or (created_at = :cursor_created_at and uuid < cast(:cursor_uuid as uuid))
			)
		`
		arguments["cursor_created_at"] = cursor.CreatedAt
		arguments["cursor_uuid"] = cursor.UUID
	} else if params.BeforeID != "" {
		query += `
			and (
				created_at > :cursor_created_at
				or (created_at = :cursor_created_at and uuid > cast(:cursor_uuid as uuid))
			)
		`
		arguments["cursor_created_at"] = cursor.CreatedAt
		arguments["cursor_uuid"] = cursor.UUID
	}
	if params.BeforeID != "" {
		query += " order by created_at asc, uuid asc limit :limit"
	} else {
		query += " order by created_at desc, uuid desc limit :limit"
	}
	return query, arguments
}

func softDeleteFileSQLX(
	ctx context.Context,
	database *sqlx.DB,
	workspaceUUID string,
	fileExternalID string,
) error {
	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	arguments := getFileArguments(workspaceUUID, fileExternalID)
	if _, err := namedExecContext(ctx, tx, fileWorkspaceLockQuery, arguments); err != nil {
		return err
	}
	file, err := getFileRecordSQLX(ctx, tx, softDeleteFileRecordQuery, arguments)
	if err != nil {
		return err
	}
	arguments["file_uuid"] = file.UUID
	var referenced bool
	if err := namedGetContext(ctx, tx, &referenced, activeFileReferenceQuery, arguments); err != nil {
		return err
	}
	if referenced {
		return ErrFileInUse
	}
	if _, err := namedExecContext(ctx, tx, softDeleteFileQuery, arguments); err != nil {
		return err
	}
	if err := applyWorkspaceStorageDeltaSQLXTx(ctx, tx, workspaceUUID, -file.SizeBytes, 0, 0); err != nil {
		return err
	}
	return tx.Commit()
}

func enqueueObjectCleanupResourceJobSQLX(
	ctx context.Context,
	database sqlxNamedExecer,
	workspaceUUID string,
	bucket, objectKey, resourceType, resourceID string,
) error {
	_, err := namedExecContext(ctx, database, enqueueObjectCleanupResourceJobQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
		"bucket":         bucket,
		"object_key":     objectKey,
		"resource_type":  resourceType,
		"resource_id":    resourceID,
	})
	return err
}

func leaseObjectCleanupJobsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	workerID string,
	limit int,
) ([]ObjectCleanupJob, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows []objectCleanupJobRow
	if err := namedSelectContext(ctx, database, &rows, leaseObjectCleanupJobsQuery, map[string]any{
		"limit":     limit,
		"worker_id": workerID,
	}); err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, nil
	}
	jobs := make([]ObjectCleanupJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs, nil
}

func completeObjectCleanupJobSQLX(
	ctx context.Context,
	database sqlxNamedExecer,
	jobUUID string,
) error {
	_, err := namedExecContext(
		ctx,
		database,
		completeObjectCleanupJobQuery,
		map[string]any{"job_uuid": jobUUID},
	)
	return err
}

func failObjectCleanupJobSQLX(
	ctx context.Context,
	database sqlxNamedExecer,
	jobUUID string,
	attempts int,
	reason string,
	retryDelay time.Duration,
	maxAttempts int,
) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	_, err := namedExecContext(ctx, database, failObjectCleanupJobQuery, map[string]any{
		"job_uuid":  jobUUID,
		"status":    status,
		"run_after": time.Now().UTC().Add(retryDelay),
		"attempts":  nextAttempts,
		"reason":    reason,
	})
	return err
}

func (r fileRecordRow) record() FileRecord {
	return FileRecord{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		WorkspaceUUID:       r.WorkspaceUUID,
		Filename:            r.Filename,
		MimeType:            r.MimeType,
		SizeBytes:           r.SizeBytes,
		SHA256:              r.SHA256,
		S3Bucket:            r.S3Bucket,
		S3Key:               r.S3Key,
		Downloadable:        r.Downloadable,
		ScopeType:           r.ScopeType,
		ScopeID:             r.ScopeID,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		CreatedAt:           r.CreatedAt,
	}
}

func (r objectCleanupJobRow) job() ObjectCleanupJob {
	return ObjectCleanupJob{
		UUID:           r.UUID,
		ExternalID:     r.ExternalID,
		WorkspaceUUID:  r.WorkspaceUUID,
		Bucket:         r.Bucket,
		Key:            r.Key,
		FileExternalID: r.FileExternalID,
		Attempts:       r.Attempts,
	}
}
