package db

import (
	"context"
	"github.com/superduck-ai/yourbatis"
)

// EventPayloadBlob describes immutable event bytes stored outside PostgreSQL.
// Each upload gets its own UUID; attachment and cleanup serialize on that row.
type EventPayloadBlob struct {
	UUID             string
	ExternalID       string
	OrganizationUUID string
	WorkspaceUUID    string
	Bucket           string
	Key              string
	Size             int64
	SHA256           string
}

func (d *DB) RegisterEventPayloadBlob(ctx context.Context, blob EventPayloadBlob) error {
	return NewEventPayloadBlobMapper(d.mapperDB).Insert(ctx, blob)
}

func (d *DB) GetEventPayloadBlob(ctx context.Context, workspaceUUID, blobUUID string) (EventPayloadBlob, error) {
	row, err := NewEventPayloadBlobMapper(d.mapperDB).Find(ctx, workspaceUUID, blobUUID)
	return EventPayloadBlob(row), mapNoRows(err)
}

func attachEventPayloadBlob(ctx context.Context, executor yourbatis.Executor, workspaceUUID string, blobUUID *string) error {
	if blobUUID == nil {
		return nil
	}
	rows, err := NewEventPayloadBlobMapper(executor).Attach(ctx, workspaceUUID, *blobUUID)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrInvalidState
	}
	return nil
}

// ScheduleEventPayloadCleanup atomically claims unreferenced objects and enqueues
// one deletion job per object. Failures use the job’s existing retry policy.
// Deleting tombstones cannot be attached or claimed again.
func (d *DB) ScheduleEventPayloadCleanup(ctx context.Context, limit int) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		blobs, err := NewEventPayloadBlobMapper(executor).ClaimCleanup(ctx, limit)
		if err != nil {
			return err
		}
		for _, blob := range blobs {
			payload, err := objectCleanupJobPayload(blob.Bucket, blob.Key, "event_payload", blob.ExternalID)
			if err != nil {
				return err
			}
			jobs := NewObjectCleanupJobMapper(executor)
			if err := jobs.EnqueueObjectCleanupJob(ctx, blob.WorkspaceUUID, payload); err != nil {
				return err
			}
		}
		return nil
	})
}
