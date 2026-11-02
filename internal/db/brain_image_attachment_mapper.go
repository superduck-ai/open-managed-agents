package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper BrainImageAttachmentMapper -sql ./brain_image_attachment_mapper.xml -out ./brain_image_attachment_mapper.sqlmap.gen.go -dialect postgres

type BrainImageAttachmentMapper interface {
	Insert(ctx context.Context, params BrainImageAttachment) (int64, error)
	Find(ctx context.Context, workspaceUUID, sessionUUID, digest string, lock bool) (brainImageAttachmentRow, bool, error)
	Attach(ctx context.Context, workspaceUUID, sessionUUID, digest string) (int64, error)
	ScheduleCleanup(ctx context.Context, limit int) ([]brainImageAttachmentRow, error)
	DeleteCompleted(ctx context.Context, jobUUID string) error
}

type brainImageAttachmentRow struct {
	WorkspaceUUID string `db:"workspace_uuid"`
	SessionUUID   string `db:"session_uuid"`
	SHA256        string `db:"sha256"`
	MediaType     string `db:"media_type"`
	SizeBytes     int64  `db:"size_bytes"`
	Bucket        string `db:"bucket"`
	ObjectKey     string `db:"object_key"`
	State         string `db:"state"`
}
