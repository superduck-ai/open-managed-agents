package db

import (
	"context"
	"time"
)

type FileRecord struct {
	UUID                string
	ExternalID          string
	WorkspaceUUID       string
	Filename            string
	MimeType            string
	SizeBytes           int64
	SHA256              string
	S3Bucket            string
	S3Key               string
	Downloadable        bool
	ScopeType           *string
	ScopeID             *string
	CreatedByAPIKeyUUID string
	CreatedAt           time.Time
}

type ListFilesPageParams struct {
	WorkspaceUUID string
	ScopeID       string
	AfterID       string
	BeforeID      string
	Limit         int
}

type ObjectCleanupJob struct {
	UUID           string
	ExternalID     string
	WorkspaceUUID  string
	Bucket         string
	Key            string
	FileExternalID string
	Attempts       int
}

func (d *DB) WorkspaceStorageBytes(ctx context.Context, workspaceUUID string) (int64, error) {
	return workspaceStorageBytesQuery(ctx, d.sql, workspaceUUID)
}

func (d *DB) CreateFile(ctx context.Context, f FileRecord) error {
	return createFileSQLX(ctx, d.sql, f)
}

func (d *DB) CreateFileIfWithinLimit(ctx context.Context, f FileRecord, workspaceStorageLimitBytes int64) error {
	return createFileIfWithinLimitSQLX(ctx, d.sql, f, workspaceStorageLimitBytes)
}

func (d *DB) GetFile(ctx context.Context, workspaceUUID string, fileExternalID string) (FileRecord, error) {
	return getFileRecordSQLX(ctx, d.sql, getFileQuery, getFileArguments(workspaceUUID, fileExternalID))
}

func (d *DB) GetFileByUUID(ctx context.Context, workspaceUUID string, fileUUID string) (FileRecord, error) {
	return getFileRecordSQLX(
		ctx,
		d.sql,
		getFileByUUIDQuery,
		fileUUIDArguments(workspaceUUID, fileUUID),
	)
}

func (d *DB) GetFileByUUIDInOrganization(ctx context.Context, organizationUUID string, fileUUID string) (FileRecord, error) {
	return getFileRecordSQLX(
		ctx,
		d.sql,
		getFileByUUIDInOrganizationQuery,
		map[string]any{
			"organization_uuid": organizationUUID,
			"file_uuid":         fileUUID,
		},
	)
}

func (d *DB) ListFiles(ctx context.Context, workspaceUUID string, scopeID string) ([]FileRecord, error) {
	query, arguments := listFilesSQLXQuery(workspaceUUID, scopeID)
	return listFileRecordsSQLX(ctx, d.sql, query, arguments)
}

func (d *DB) ListFilesPage(ctx context.Context, params ListFilesPageParams) ([]FileRecord, bool, error) {
	return listFilesPageSQLX(ctx, d.sql, params)
}

func (d *DB) SoftDeleteFile(ctx context.Context, workspaceUUID string, fileExternalID string) error {
	return softDeleteFileSQLX(ctx, d.sql, workspaceUUID, fileExternalID)
}

func (d *DB) EnqueueObjectCleanupJob(ctx context.Context, workspaceUUID string, bucket, key, fileExternalID string) error {
	return d.EnqueueObjectCleanupResourceJob(ctx, workspaceUUID, bucket, key, "file", fileExternalID)
}

func (d *DB) EnqueueObjectCleanupResourceJob(ctx context.Context, workspaceUUID string, bucket, key, resourceType, resourceID string) error {
	return enqueueObjectCleanupResourceJobSQLX(
		ctx,
		d.sql,
		workspaceUUID,
		bucket,
		key,
		resourceType,
		resourceID,
	)
}

func (d *DB) LeaseObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]ObjectCleanupJob, error) {
	return leaseObjectCleanupJobsSQLX(ctx, d.sql, workerID, limit)
}

func (d *DB) CompleteObjectCleanupJob(ctx context.Context, jobUUID string) error {
	return completeObjectCleanupJobSQLX(ctx, d.sql, jobUUID)
}

func (d *DB) FailObjectCleanupJob(ctx context.Context, jobUUID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	return failObjectCleanupJobSQLX(ctx, d.sql, jobUUID, attempts, reason, retryDelay, maxAttempts)
}
