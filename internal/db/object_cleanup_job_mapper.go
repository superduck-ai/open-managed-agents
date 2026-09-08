package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ObjectCleanupJobMapper -sql ./object_cleanup_job_mapper.xml -out ./object_cleanup_job_mapper.sqlmap.gen.go -dialect postgres

type ObjectCleanupJobMapper interface {
	EnqueueObjectCleanupJob(ctx context.Context, workspaceUUID string, payload []byte) error
	EnqueueScheduledObjectCleanupJob(ctx context.Context, params scheduledObjectCleanupJobParams) error
	ExpediteObjectCleanupJob(ctx context.Context, externalID string) (int64, error)
	LeaseObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]objectCleanupJobRow, error)
	CompleteObjectCleanupJob(ctx context.Context, jobUUID string) error
	FailObjectCleanupJob(ctx context.Context, params objectCleanupJobFailureParams) error
}

type objectCleanupJobFailureParams struct {
	JobUUID  string
	Status   string
	RunAfter time.Time
	Attempts int
	Reason   string
}

type scheduledObjectCleanupJobParams struct {
	ExternalID    string
	WorkspaceUUID string
	Payload       []byte
	RunAfter      time.Time
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
