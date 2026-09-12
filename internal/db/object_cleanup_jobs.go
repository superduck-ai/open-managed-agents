package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type ObjectCleanupJob struct {
	UUID           string
	ExternalID     string
	WorkspaceUUID  string
	Bucket         string
	Key            string
	FileExternalID string
	Attempts       int
}

func (d *DB) EnqueueObjectCleanupJob(ctx context.Context, workspaceUUID string, bucket, key, fileExternalID string) error {
	return d.EnqueueObjectCleanupResourceJob(ctx, workspaceUUID, bucket, key, "file", fileExternalID)
}

func (d *DB) EnqueueObjectCleanupResourceJob(ctx context.Context, workspaceUUID string, bucket, key, resourceType, resourceID string) error {
	payload, err := objectCleanupJobPayload(bucket, key, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("encode object cleanup job payload: %w", err)
	}
	return NewObjectCleanupJobMapper(d.mapperDB).EnqueueObjectCleanupJob(ctx, workspaceUUID, payload)
}

func (d *DB) EnqueueScheduledObjectCleanupResourceJob(
	ctx context.Context,
	externalID string,
	workspaceUUID string,
	bucket string,
	key string,
	resourceType string,
	resourceID string,
	runAfter time.Time,
) error {
	payload, err := objectCleanupJobPayload(bucket, key, resourceType, resourceID)
	if err != nil {
		return err
	}
	return NewObjectCleanupJobMapper(d.mapperDB).EnqueueScheduledObjectCleanupJob(ctx, scheduledObjectCleanupJobParams{
		ExternalID: externalID, WorkspaceUUID: workspaceUUID, Payload: payload, RunAfter: runAfter,
	})
}

func (d *DB) ExpediteObjectCleanupJob(ctx context.Context, externalID string) error {
	rows, err := NewObjectCleanupJobMapper(d.mapperDB).ExpediteObjectCleanupJob(ctx, externalID)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) LeaseObjectCleanupJobs(ctx context.Context, workerID string, limit int) ([]ObjectCleanupJob, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := NewObjectCleanupJobMapper(d.mapperDB).LeaseObjectCleanupJobs(ctx, workerID, limit)
	if err != nil {
		return nil, err
	}
	jobs := make([]ObjectCleanupJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs, nil
}

func (d *DB) CompleteObjectCleanupJob(ctx context.Context, jobUUID string) error {
	return NewObjectCleanupJobMapper(d.mapperDB).CompleteObjectCleanupJob(ctx, jobUUID)
}

func (d *DB) FailObjectCleanupJob(ctx context.Context, jobUUID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	return NewObjectCleanupJobMapper(d.mapperDB).FailObjectCleanupJob(ctx, objectCleanupJobFailureParams{
		JobUUID:  jobUUID,
		Status:   status,
		RunAfter: time.Now().UTC().Add(retryDelay),
		Attempts: nextAttempts,
		Reason:   reason,
	})
}

func objectCleanupJobPayload(bucket, objectKey, resourceType, resourceID string) ([]byte, error) {
	fileExternalID := ""
	if resourceType == "file" {
		fileExternalID = resourceID
	}
	return json.Marshal(map[string]string{
		"bucket": bucket, "key": objectKey, "file_id": fileExternalID,
		"resource_type": resourceType, "resource_id": resourceID,
	})
}
