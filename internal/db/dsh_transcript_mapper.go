package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper DSHTranscriptMapper -sql ./dsh_transcript_mapper.xml -out ./dsh_transcript_mapper.sqlmap.gen.go -dialect postgres

type dshTranscriptRow struct {
	UUID               string  `db:"uuid"`
	DSHSessionID       string  `db:"dsh_session_id"`
	ParentDSHSessionID *string `db:"parent_dsh_session_id"`
	SessionThreadID    *string `db:"session_thread_id"`
	NextOffset         int64   `db:"next_offset"`
}

type dshTranscriptRecordRow struct {
	Offset  int64  `db:"record_offset"`
	Payload []byte `db:"payload"`
}

type dshWorkerScopeRow struct {
	UUID        string `db:"uuid"`
	WorkerEpoch int64  `db:"current_worker_epoch"`
	LeaseValid  bool   `db:"lease_valid"`
}

type dshTranscriptInsert struct {
	Scope              DSHScope
	DSHSessionID       string
	ParentDSHSessionID *string
}

type dshTranscriptRecordsInsert struct {
	Scope          DSHScope
	TranscriptUUID string
	Records        []DSHTranscriptRecord
}

type DSHTranscriptMapper interface {
	ReadWorker(ctx context.Context, scope DSHScope) (dshWorkerScopeRow, bool, error)
	ShareWorker(ctx context.Context, scope DSHScope) (dshWorkerScopeRow, bool, error)
	LockHeartbeat(ctx context.Context, scope DSHScope) (dshWorkerScopeRow, bool, error)
	LockTranscript(ctx context.Context, scope DSHScope, dshSessionID string) (dshTranscriptRow, bool, error)
	LockWorker(ctx context.Context, scope DSHScope) (dshWorkerScopeRow, bool, error)
	Find(ctx context.Context, scope DSHScope, dshSessionID string) (dshTranscriptRow, bool, error)
	Insert(ctx context.Context, input dshTranscriptInsert) (int64, error)
	List(ctx context.Context, scope DSHScope, after string, limit int) ([]dshTranscriptRow, error)
	ReadRecords(ctx context.Context, scope DSHScope, transcriptUUID string, fromOffset int64, limit int) ([]dshTranscriptRecordRow, error)
	ReadRecordPage(ctx context.Context, scope DSHScope, transcriptUUID string, fromOffset int64, limit int, budget int64) ([]dshTranscriptRecordRow, error)
	InsertRecords(ctx context.Context, input dshTranscriptRecordsInsert) (int64, error)
	SetNextOffset(ctx context.Context, scope DSHScope, transcriptUUID string, nextOffset int64) (int64, error)
}
