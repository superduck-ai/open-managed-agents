package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type FileRecord struct {
	ID                int64
	UUID              string
	ExternalID        string
	WorkspaceID       int64
	Filename          string
	MimeType          string
	SizeBytes         int64
	SHA256            string
	S3Bucket          string
	S3Key             string
	Downloadable      bool
	ScopeType         *string
	ScopeID           *string
	CreatedByAPIKeyID int64
	CreatedAt         time.Time
}

type ListFilesPageParams struct {
	WorkspaceID int64
	ScopeID     string
	AfterID     string
	BeforeID    string
	Limit       int
}

type ObjectCleanupJob struct {
	ID             int64
	ExternalID     string
	WorkspaceID    int64
	Bucket         string
	Key            string
	FileExternalID string
	Attempts       int
}

func (d *DB) WorkspaceStorageBytes(ctx context.Context, workspaceID int64) (int64, error) {
	return workspaceStorageBytesQuery(ctx, d.sql, workspaceID)
}

func (d *DB) CreateFile(ctx context.Context, f FileRecord) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, f.WorkspaceID); err != nil {
		return err
	}
	if err := insertFileTx(ctx, tx, f); err != nil {
		return err
	}
	if err := applyWorkspaceStorageDeltaTx(ctx, tx, f.WorkspaceID, f.SizeBytes, 0, 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) CreateFileIfWithinLimit(ctx context.Context, f FileRecord, workspaceStorageLimitBytes int64) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, f.WorkspaceID); err != nil {
		return err
	}

	if err := applyWorkspaceStorageDeltaTx(ctx, tx, f.WorkspaceID, f.SizeBytes, 0, workspaceStorageLimitBytes); err != nil {
		return err
	}
	if err := insertFileTx(ctx, tx, f); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertFileTx(ctx context.Context, tx pgx.Tx, f FileRecord) error {
	_, err := tx.Exec(ctx, `
		insert into files (
			uuid, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, f.UUID, f.ExternalID, f.WorkspaceID, f.Filename, f.MimeType, f.SizeBytes, f.SHA256,
		f.S3Bucket, f.S3Key, f.Downloadable, f.ScopeType, f.ScopeID, f.CreatedByAPIKeyID, f.CreatedAt)
	return err
}

func (d *DB) GetFile(ctx context.Context, workspaceID int64, fileExternalID string) (FileRecord, error) {
	var f FileRecord
	err := d.Pool.QueryRow(ctx, `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, fileExternalID).Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
		&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileRecord{}, ErrNotFound
	}
	return f, err
}

func (d *DB) GetFileByUUID(ctx context.Context, workspaceID int64, fileUUID string) (FileRecord, error) {
	var f FileRecord
	err := d.Pool.QueryRow(ctx, `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and uuid::text = $2 and deleted_at is null
	`, workspaceID, fileUUID).Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
		&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileRecord{}, ErrNotFound
	}
	return f, err
}

func (d *DB) GetFileByUUIDInOrganization(ctx context.Context, organizationID int64, fileUUID string) (FileRecord, error) {
	var f FileRecord
	err := d.Pool.QueryRow(ctx, `
		select f.id, f.uuid::text, f.external_id, f.workspace_id, f.filename, f.mime_type, f.size_bytes, f.sha256,
			f.s3_bucket, f.s3_key, f.downloadable, f.scope_type, f.scope_id, f.created_by_api_key_id, f.created_at
		from files f
		join workspaces w on w.id = f.workspace_id
		where w.organization_id = $1 and f.uuid::text = $2 and f.deleted_at is null
	`, organizationID, fileUUID).Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
		&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FileRecord{}, ErrNotFound
	}
	return f, err
}

func (d *DB) ListFiles(ctx context.Context, workspaceID int64, scopeID string) ([]FileRecord, error) {
	query := `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{workspaceID}
	if scopeID != "" {
		query += " and scope_id = $2"
		args = append(args, scopeID)
	}
	query += " order by created_at desc, id desc"

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
			&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

func (d *DB) ListFilesPage(ctx context.Context, params ListFilesPageParams) ([]FileRecord, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.AfterID != "" {
		params.BeforeID = ""
	}

	var cursorID int64
	var cursorCreatedAt time.Time
	if params.AfterID != "" || params.BeforeID != "" {
		cursorExternalID := params.AfterID
		if cursorExternalID == "" {
			cursorExternalID = params.BeforeID
		}
		query := `
			select id, created_at
			from files
			where workspace_id = $1 and external_id = $2 and deleted_at is null
		`
		args := []any{params.WorkspaceID, cursorExternalID}
		if params.ScopeID != "" {
			query += " and scope_id = $3"
			args = append(args, params.ScopeID)
		}
		err := d.Pool.QueryRow(ctx, query, args...).Scan(&cursorID, &cursorCreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}

	query := `
		select id, uuid::text, external_id, workspace_id, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_id, created_at
		from files
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if params.ScopeID != "" {
		query += fmt.Sprintf(" and scope_id = $%d", nextArg)
		args = append(args, params.ScopeID)
		nextArg++
	}
	if params.AfterID != "" {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, cursorCreatedAt, cursorID)
		nextArg += 2
	} else if params.BeforeID != "" {
		query += fmt.Sprintf(" and (created_at > $%d or (created_at = $%d and id > $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, cursorCreatedAt, cursorID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	files, err := scanFileRows(rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(files) > params.Limit
	if hasMore {
		files = files[:params.Limit]
	}
	return files, hasMore, nil
}

func (d *DB) SoftDeleteFile(ctx context.Context, workspaceID int64, fileExternalID string) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, workspaceID); err != nil {
		return err
	}
	var sizeBytes int64
	err = tx.QueryRow(ctx, `
		select size_bytes
		from files
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, fileExternalID).Scan(&sizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		update files
		set deleted_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, fileExternalID); err != nil {
		return err
	}
	if err := applyWorkspaceStorageDeltaTx(ctx, tx, workspaceID, -sizeBytes, 0, 0); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) EnqueueObjectCleanupJob(ctx context.Context, workspaceID int64, bucket, key, fileExternalID string) error {
	return d.EnqueueObjectCleanupResourceJob(ctx, workspaceID, bucket, key, "file", fileExternalID)
}

func (d *DB) EnqueueObjectCleanupResourceJob(ctx context.Context, workspaceID int64, bucket, key, resourceType, resourceID string) error {
	_, err := d.Pool.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1,
			'object_cleanup',
			'pending',
			jsonb_build_object(
				'bucket', $2::text,
				'key', $3::text,
				'file_id', case when $4::text = 'file' then $5::text else '' end,
				'resource_type', $4::text,
				'resource_id', $5::text
			)
		)
	`, workspaceID, bucket, key, resourceType, resourceID)
	return err
}

func (d *DB) LeaseObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]ObjectCleanupJob, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := d.Pool.Query(ctx, `
		with next_jobs as (
			select id
			from jobs
			where type = 'object_cleanup'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit $1
			for update skip locked
		)
		update jobs j
		set status = 'running',
			locked_by = $2,
			locked_until = now() + interval '1 minute',
			updated_at = now()
		from next_jobs
		where j.id = next_jobs.id
		returning j.id, j.external_id, j.workspace_id,
			coalesce(j.payload->>'bucket', ''),
			coalesce(j.payload->>'key', ''),
			coalesce(j.payload->>'file_id', ''),
			j.attempts
	`, limit, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []ObjectCleanupJob
	for rows.Next() {
		var job ObjectCleanupJob
		if err := rows.Scan(&job.ID, &job.ExternalID, &job.WorkspaceID, &job.Bucket, &job.Key, &job.FileExternalID, &job.Attempts); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) CompleteObjectCleanupJob(ctx context.Context, jobID int64) error {
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where id = $1 and type = 'object_cleanup'
	`, jobID)
	return err
}

func (d *DB) FailObjectCleanupJob(ctx context.Context, jobID int64, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = $2,
			locked_by = null,
			locked_until = null,
			run_after = $3,
			updated_at = now(),
			attempts = $5,
			payload = payload || jsonb_build_object('last_error', $4::text)
		where id = $1 and type = 'object_cleanup'
	`, jobID, status, runAfter, reason, nextAttempts)
	return err
}

type fileRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanFileRows(rows fileRows) ([]FileRecord, error) {
	var files []FileRecord
	for rows.Next() {
		var f FileRecord
		if err := rows.Scan(&f.ID, &f.UUID, &f.ExternalID, &f.WorkspaceID, &f.Filename, &f.MimeType, &f.SizeBytes, &f.SHA256,
			&f.S3Bucket, &f.S3Key, &f.Downloadable, &f.ScopeType, &f.ScopeID, &f.CreatedByAPIKeyID, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
