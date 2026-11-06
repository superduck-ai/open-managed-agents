package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper DSHTranscriptMapper -sql ./dsh_transcript_mapper.xml -out ./dsh_transcript_mapper.sqlmap.gen.go -dialect postgres

type dshTranscriptRow struct {
	UUID               string  `db:"uuid"`
	DSHSessionID       string  `db:"dsh_session_id"`
	ParentDSHSessionID *string `db:"parent_dsh_session_id"`
	SessionThreadID    *string `db:"session_thread_id"`
	Header             []byte  `db:"header"`
	NextSeq            int64   `db:"next_seq"`
}

type dshTranscriptEventRow struct {
	Seq     int64  `db:"seq"`
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
	Header             []byte
}

type dshTranscriptEventsInsert struct {
	Scope          DSHScope
	TranscriptUUID string
	Events         []DSHTranscriptEvent
}

type DSHTranscriptMapper interface {
	ShareWorker(ctx context.Context, scope DSHScope) (dshWorkerScopeRow, bool, error)
	LockHeartbeat(ctx context.Context, scope DSHScope) (dshWorkerScopeRow, bool, error)
	LockTranscript(ctx context.Context, scope DSHScope, dshSessionID string) (dshTranscriptRow, bool, error)
	LockWorker(ctx context.Context, scope DSHScope) (dshWorkerScopeRow, bool, error)
	Find(ctx context.Context, scope DSHScope, dshSessionID string) (dshTranscriptRow, bool, error)
	Insert(ctx context.Context, input dshTranscriptInsert) (int64, error)
	List(ctx context.Context, scope DSHScope, after string, limit int) ([]dshTranscriptRow, error)
	ReadEvents(ctx context.Context, scope DSHScope, transcriptUUID string, fromSeq int64, limit int) ([]dshTranscriptEventRow, error)
	InsertEvents(ctx context.Context, input dshTranscriptEventsInsert) (int64, error)
	SetNextSeq(ctx context.Context, scope DSHScope, transcriptUUID string, nextSeq int64) (int64, error)
}
