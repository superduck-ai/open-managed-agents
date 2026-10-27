package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper TranscriptArchiveMapper -sql ./transcript_archive_mapper.xml -out ./transcript_archive_mapper.sqlmap.gen.go -dialect postgres

type transcriptArchiveRow struct {
	UUID                  string    `db:"uuid"`
	ExternalID            string    `db:"external_id"`
	OrganizationUUID      string    `db:"organization_uuid"`
	WorkspaceUUID         string    `db:"workspace_uuid"`
	CodeSessionUUID       string    `db:"code_session_uuid"`
	CodeSessionExternalID string    `db:"code_session_external_id"`
	FromSequence          int64     `db:"from_sequence_num"`
	ToSequence            int64     `db:"to_sequence_num"`
	EventCount            int       `db:"event_count"`
	Codec                 string    `db:"codec"`
	Bucket                string    `db:"bucket"`
	Key                   string    `db:"object_key"`
	Size                  int64     `db:"size_bytes"`
	RawBytes              int64     `db:"raw_bytes"`
	SHA256                string    `db:"sha256"`
	State                 string    `db:"state"`
	CreatedAt             time.Time `db:"created_at"`
	UpdatedAt             time.Time `db:"updated_at"`
}

type TranscriptArchiveMapper interface {
	LockScope(ctx context.Context, key string) (bool, error)
	Overlaps(ctx context.Context, archive TranscriptArchive) (bool, error)
	Insert(ctx context.Context, archive TranscriptArchive) error
	Attach(ctx context.Context, scope TranscriptScope, archiveUUID string) (int64, error)
	FindByRange(ctx context.Context, scope TranscriptScope, fromSequence int64) (transcriptArchiveRow, bool, error)
	ListBySession(ctx context.Context, scope TranscriptScope, after int64, limit int, attachedOnly bool) ([]transcriptArchiveRow, error)
	LockAttached(ctx context.Context, scope TranscriptScope, archiveUUID string) (transcriptArchiveRow, bool, error)
	HasAttachedCovering(ctx context.Context, scope TranscriptScope, archiveUUID string, sequence int64) (bool, error)
	ClaimCleanup(ctx context.Context, limit int) ([]transcriptArchiveRow, error)
}
