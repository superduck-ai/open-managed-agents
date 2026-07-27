package db

import (
	"context"
	"time"
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
	return createFileSQLX(ctx, d.sql, f)
}

func (d *DB) CreateFileIfWithinLimit(ctx context.Context, f FileRecord, workspaceStorageLimitBytes int64) error {
	return createFileIfWithinLimitSQLX(ctx, d.sql, f, workspaceStorageLimitBytes)
}

func (d *DB) GetFile(ctx context.Context, workspaceID int64, fileExternalID string) (FileRecord, error) {
	return getFileRecordSQLX(ctx, d.sql, getFileQuery, getFileArguments(workspaceID, fileExternalID))
}

func (d *DB) GetFileByUUID(ctx context.Context, workspaceID int64, fileUUID string) (FileRecord, error) {
	return getFileRecordSQLX(
		ctx,
		d.sql,
		getFileByUUIDQuery,
		fileUUIDArguments(workspaceID, fileUUID),
	)
}

func (d *DB) GetFileByUUIDInOrganization(ctx context.Context, organizationID int64, fileUUID string) (FileRecord, error) {
	return getFileRecordSQLX(
		ctx,
		d.sql,
		getFileByUUIDInOrganizationQuery,
		map[string]any{
			"organization_id": organizationID,
			"file_uuid":       fileUUID,
		},
	)
}

func (d *DB) ListFiles(ctx context.Context, workspaceID int64, scopeID string) ([]FileRecord, error) {
	query, arguments := listFilesSQLXQuery(workspaceID, scopeID)
	return listFileRecordsSQLX(ctx, d.sql, query, arguments)
}

func (d *DB) ListFilesPage(ctx context.Context, params ListFilesPageParams) ([]FileRecord, bool, error) {
	return listFilesPageSQLX(ctx, d.sql, params)
}

func (d *DB) SoftDeleteFile(ctx context.Context, workspaceID int64, fileExternalID string) error {
	return softDeleteFileSQLX(ctx, d.sql, workspaceID, fileExternalID)
}

func (d *DB) EnqueueObjectCleanupJob(ctx context.Context, workspaceID int64, bucket, key, fileExternalID string) error {
	return d.EnqueueObjectCleanupResourceJob(ctx, workspaceID, bucket, key, "file", fileExternalID)
}

func (d *DB) EnqueueObjectCleanupResourceJob(ctx context.Context, workspaceID int64, bucket, key, resourceType, resourceID string) error {
	return enqueueObjectCleanupResourceJobSQLX(
		ctx,
		d.sql,
		workspaceID,
		bucket,
		key,
		resourceType,
		resourceID,
	)
}

func (d *DB) LeaseObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]ObjectCleanupJob, error) {
	return leaseObjectCleanupJobsSQLX(ctx, d.sql, workerID, limit)
}

func (d *DB) CompleteObjectCleanupJob(ctx context.Context, jobID int64) error {
	return completeObjectCleanupJobSQLX(ctx, d.sql, jobID)
}

func (d *DB) FailObjectCleanupJob(ctx context.Context, jobID int64, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	return failObjectCleanupJobSQLX(ctx, d.sql, jobID, attempts, reason, retryDelay, maxAttempts)
}
