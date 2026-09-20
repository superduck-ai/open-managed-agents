package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper DreamSessionTranscriptMapper -sql ./dream_session_transcript_mapper.xml -out ./dream_session_transcript_mapper.sqlmap.gen.go -dialect postgres

type dreamSessionTranscriptRow struct {
	UUID                    string    `db:"uuid"`
	DreamUUID               string    `db:"dream_uuid"`
	WorkspaceUUID           string    `db:"workspace_uuid"`
	SourceSessionUUID       string    `db:"source_session_uuid"`
	SourceSessionExternalID string    `db:"source_session_external_id"`
	Ordinal                 int       `db:"ordinal"`
	CreatedAt               time.Time `db:"created_at"`
}

type insertDreamSessionTranscriptParams struct {
	UUID                    string
	DreamUUID               string
	WorkspaceUUID           string
	SourceSessionUUID       string
	SourceSessionExternalID string
	Ordinal                 int
	CreatedAt               time.Time
}

type DreamSessionTranscriptMapper interface {
	Insert(ctx context.Context, params insertDreamSessionTranscriptParams) (dreamSessionTranscriptRow, error)
	ListByDreamUUID(ctx context.Context, workspaceUUID, dreamUUID string) ([]dreamSessionTranscriptRow, error)
}
