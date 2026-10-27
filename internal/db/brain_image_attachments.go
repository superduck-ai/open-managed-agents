package db

import (
	"context"
	"github.com/superduck-ai/yourbatis"
)

type BrainImageAttachment struct {
	OrganizationUUID string
	WorkspaceUUID    string
	SessionUUID      string
	SHA256           string
	MediaType        string
	SizeBytes        int64
	Bucket           string
	ObjectKey        string
}

func (d *DB) GetBrainImageAttachment(ctx context.Context, workspaceUUID, sessionUUID, digest string) (BrainImageAttachment, error) {
	row, found, err := NewBrainImageAttachmentMapper(d.mapperDB).Find(ctx, workspaceUUID, sessionUUID, digest, false)
	if err != nil {
		return BrainImageAttachment{}, err
	}
	if !found || row.State != "attached" {
		return BrainImageAttachment{}, ErrNotFound
	}
	return BrainImageAttachment{WorkspaceUUID: row.WorkspaceUUID, SessionUUID: row.SessionUUID, SHA256: row.SHA256, MediaType: row.MediaType, SizeBytes: row.SizeBytes, Bucket: row.Bucket, ObjectKey: row.ObjectKey}, nil
}

func (d *DB) PutBrainImageAttachment(ctx context.Context, input BrainImageAttachment, upload func() error) (bool, error) {
	if _, err := NewBrainImageAttachmentMapper(d.mapperDB).Insert(ctx, input); err != nil {
		return false, err
	}
	created := false
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewBrainImageAttachmentMapper(executor)
		row, found, err := mapper.Find(ctx, input.WorkspaceUUID, input.SessionUUID, input.SHA256, true)
		if err != nil {
			return err
		}
		if !found || row.State == "deleting" {
			return ErrNotFound
		}
		if row.State == "attached" {
			return nil
		}
		if err := upload(); err != nil {
			return err
		}
		rows, err := mapper.Attach(ctx, input.WorkspaceUUID, input.SessionUUID, input.SHA256)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrInvalidState
		}
		created = true
		return nil
	})
	return created, err
}

func (d *DB) ScheduleBrainImageAttachmentCleanup(ctx context.Context, limit int) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		rows, err := NewBrainImageAttachmentMapper(executor).ScheduleCleanup(ctx, limit)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := enqueueObjectCleanupRef(ctx, executor, ObjectRef{WorkspaceUUID: row.WorkspaceUUID, Bucket: row.Bucket, Key: row.ObjectKey, ResourceType: "brain_image_attachment", ResourceID: row.SHA256}); err != nil {
				return err
			}
		}
		return nil
	})
}
