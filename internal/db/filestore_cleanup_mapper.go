package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper FilestoreCleanupMapper -sql ./filestore_cleanup_mapper.xml -out ./filestore_cleanup_mapper.sqlmap.gen.go -dialect postgres

type FilestoreCleanupMapper interface {
	GetLeasedFilesystemJob(ctx context.Context, params filestoreCleanupJobLeaseIdentity) (filestoreFilesystemCleanupJobRow, error)
	GetFilesystemForCleanup(ctx context.Context, workspaceUUID, filesystemUUID string) (filestoreFilesystemRow, error)
	ListFilesystemFiles(ctx context.Context, params filestoreFilesystemBatchMapperParams) ([]sessionResourceFileRow, error)
	FilesystemFilesRemain(ctx context.Context, workspaceUUID, sessionUUID string) (bool, error)
	RetireSkillFiles(ctx context.Context, params filestoreFilesystemBatchMapperParams) error
	RetireNamespace(ctx context.Context, params filestoreFilesystemBatchMapperParams) error
	CompleteFilesystemBatch(ctx context.Context, params filestoreFilesystemBatchMapperParams) (int64, error)

	InsertFilesystemJob(ctx context.Context, params filestoreCleanupJobInsertParams) (filestoreFilesystemCleanupJobRow, error)
	InsertObjectJob(ctx context.Context, params filestoreCleanupJobInsertParams) (filestoreObjectCleanupJobRow, error)
	LeaseFilesystemJobs(ctx context.Context, params filestoreCleanupJobLeaseParams) ([]filestoreFilesystemCleanupJobRow, error)
	LeaseObjectJobs(ctx context.Context, params filestoreCleanupJobLeaseParams) ([]filestoreObjectCleanupJobRow, error)
	AttachObjectVersion(ctx context.Context, params filestoreCleanupJobMutationParams) (int64, error)
	CompletePendingObjectJob(ctx context.Context, params filestoreCleanupJobMutationParams) (int64, error)
	CompleteLeasedObjectJob(ctx context.Context, params filestoreCleanupJobMutationParams) (int64, error)
	FailLeasedJob(ctx context.Context, params filestoreCleanupJobMutationParams) (int64, error)
	CancelPendingObjectJob(ctx context.Context, params filestoreCleanupJobMutationParams) (int64, error)
	GetObjectJobStatus(ctx context.Context, params filestoreCleanupJobMutationParams) (string, error)
	CancelAttachedObjectJob(ctx context.Context, params filestoreCleanupJobMutationParams) (int64, error)

	ListSubtreeFiles(ctx context.Context, params filestoreSubtreeMapperParams) ([]sessionResourceFileRow, error)
	ListExpiredSubtreeFiles(ctx context.Context, params filestoreSubtreeMapperParams) ([]sessionResourceFileRow, error)
}

type filestoreCleanupJobLeaseIdentity struct {
	JobUUID    string
	JobType    string
	LeaseToken string
}

type filestoreFilesystemBatchMapperParams struct {
	JobUUID        string
	JobType        string
	LeaseToken     string
	WorkspaceUUID  string
	FilesystemUUID string
	SessionUUID    string
	Limit          int
	RetiredAt      time.Time
	Status         string
}

type filestoreCleanupJobInsertParams struct {
	WorkspaceUUID   string
	FilesystemUUID  string
	JobType         string
	EntryExternalID string
	Bucket          string
	Key             string
	ETag            string
	VersionID       string
	Reason          string
	RunAfter        time.Time
}

type filestoreCleanupJobLeaseParams struct {
	JobType          string
	WorkerID         string
	Limit            int
	MaxLeaseAttempts int
}

type filestoreCleanupJobMutationParams struct {
	JobUUID       string
	JobExternalID string
	JobType       string
	WorkspaceUUID string
	LeaseToken    string
	ETag          string
	VersionID     string
	Bucket        string
	Key           string
	Reason        string
	RunAfter      time.Time
	MaxAttempts   int
}

type filestoreSubtreeMapperParams struct {
	WorkspaceUUID string
	SessionUUID   string
	RootPath      string
}

type filestoreObjectCleanupJobRow struct {
	UUID                 uuid.UUID `db:"uuid"`
	ExternalID           string    `db:"external_id"`
	WorkspaceUUID        uuid.UUID `db:"workspace_uuid"`
	FilesystemUUID       uuid.UUID `db:"filesystem_uuid"`
	FilesystemExternalID string    `db:"filesystem_external_id"`
	EntryExternalID      string    `db:"entry_external_id"`
	Bucket               string    `db:"bucket"`
	Key                  string    `db:"key"`
	ETag                 string    `db:"etag"`
	VersionID            string    `db:"version_id"`
	Reason               string    `db:"reason"`
	Attempts             int       `db:"attempts"`
	RunAfter             time.Time `db:"run_after"`
}

type filestoreFilesystemCleanupJobRow struct {
	UUID                 uuid.UUID `db:"uuid"`
	ExternalID           string    `db:"external_id"`
	WorkspaceUUID        uuid.UUID `db:"workspace_uuid"`
	FilesystemUUID       uuid.UUID `db:"filesystem_uuid"`
	FilesystemExternalID string    `db:"filesystem_external_id"`
	Attempts             int       `db:"attempts"`
	RunAfter             time.Time `db:"run_after"`
}

func (row filestoreObjectCleanupJobRow) job() FilestoreObjectCleanupJob {
	return FilestoreObjectCleanupJob{
		UUID:                 row.UUID.String(),
		ExternalID:           row.ExternalID,
		WorkspaceUUID:        row.WorkspaceUUID.String(),
		FilesystemUUID:       row.FilesystemUUID.String(),
		FilesystemExternalID: row.FilesystemExternalID,
		EntryExternalID:      row.EntryExternalID,
		Bucket:               row.Bucket,
		Key:                  row.Key,
		ETag:                 row.ETag,
		VersionID:            row.VersionID,
		Reason:               row.Reason,
		Attempts:             row.Attempts,
		RunAfter:             row.RunAfter,
	}
}

func (row filestoreFilesystemCleanupJobRow) job() FilestoreFilesystemCleanupJob {
	return FilestoreFilesystemCleanupJob{
		UUID:                 row.UUID.String(),
		ExternalID:           row.ExternalID,
		WorkspaceUUID:        row.WorkspaceUUID.String(),
		FilesystemUUID:       row.FilesystemUUID.String(),
		FilesystemExternalID: row.FilesystemExternalID,
		Attempts:             row.Attempts,
		RunAfter:             row.RunAfter,
	}
}

func filestoreObjectCleanupJobsFromMapperRows(rows []filestoreObjectCleanupJobRow) []FilestoreObjectCleanupJob {
	if rows == nil {
		return nil
	}
	jobs := make([]FilestoreObjectCleanupJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs
}

func filestoreFilesystemCleanupJobsFromMapperRows(rows []filestoreFilesystemCleanupJobRow) []FilestoreFilesystemCleanupJob {
	if rows == nil {
		return nil
	}
	jobs := make([]FilestoreFilesystemCleanupJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs
}
