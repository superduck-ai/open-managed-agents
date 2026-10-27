package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper EventPayloadBlobMapper -sql ./event_payload_blob_mapper.xml -out ./event_payload_blob_mapper.sqlmap.gen.go -dialect postgres

type eventPayloadBlobRow struct {
	UUID             string `db:"uuid"`
	ExternalID       string `db:"external_id"`
	OrganizationUUID string `db:"organization_uuid"`
	WorkspaceUUID    string `db:"workspace_uuid"`
	Bucket           string `db:"bucket"`
	Key              string `db:"object_key"`
	Size             int64  `db:"size_bytes"`
	SHA256           string `db:"sha256"`
}

type EventPayloadBlobMapper interface {
	Insert(ctx context.Context, blob EventPayloadBlob) error
	Find(ctx context.Context, workspaceUUID, blobUUID string) (eventPayloadBlobRow, error)
	Attach(ctx context.Context, workspaceUUID, blobUUID string) (int64, error)
	ClaimCleanup(ctx context.Context, limit int) ([]eventPayloadBlobRow, error)
}
