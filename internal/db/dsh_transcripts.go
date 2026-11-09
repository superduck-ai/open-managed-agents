package db

import (
	"bytes"
	"context"

	"github.com/superduck-ai/yourbatis"
)

type DSHScope struct {
	CodeSessionID    string
	SessionID        string
	OrganizationUUID string
	WorkspaceUUID    string
	CodeSessionUUID  string
	Epoch            int64
}

type DSHTranscript struct {
	DSHSessionID       string
	ParentDSHSessionID *string
	SessionThreadID    *string
	NextOffset         int64
}

type DSHTranscriptRecord struct {
	Offset  int64
	Payload []byte
}

type DSHTranscriptAppendResult struct {
	NextOffset int64
}

func dshTranscriptFromRow(row dshTranscriptRow) DSHTranscript {
	return DSHTranscript{DSHSessionID: row.DSHSessionID, ParentDSHSessionID: row.ParentDSHSessionID,
		SessionThreadID: row.SessionThreadID, NextOffset: row.NextOffset}
}

func (d *DB) ValidateDSHPreviewWorker(ctx context.Context, scope DSHScope) error {
	return lockDSHWorker(ctx, d.mapperDB, scope)
}

func lockDSHWorker(ctx context.Context, executor yourbatis.Executor, scope DSHScope) error {
	row, found, err := NewDSHTranscriptMapper(executor).ShareWorker(ctx, scope)
	if err != nil {
		return err
	}
	return validateDSHWorker(scope, row, found)
}

func validateDSHWorker(scope DSHScope, row dshWorkerScopeRow, found bool) error {
	if !found {
		return ErrNotFound
	}
	if scope.Epoch <= 0 || row.WorkerEpoch != scope.Epoch {
		return ErrWorkerEpochMismatch
	}
	if !row.LeaseValid {
		return ErrWorkerLeaseExpired
	}
	return nil
}

func sameDSHParent(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (d *DB) AppendDSHTranscript(ctx context.Context, scope DSHScope, id string, parent *string, events []DSHTranscriptRecord) (DSHTranscriptAppendResult, error) {
	result := DSHTranscriptAppendResult{}
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(ctx, executor, scope); err != nil {
			return err
		}
		mapper := NewDSHTranscriptMapper(executor)
		if len(events) == 0 {
			return ErrInvalidState
		}
		if events[0].Offset == 0 {
			if parent != nil {
				if *parent == id {
					return ErrInvalidState
				}
				_, found, err := mapper.Find(ctx, scope, *parent)
				if err != nil {
					return err
				}
				if !found {
					return ErrNotFound
				}
			}
			if _, err := mapper.Insert(ctx, dshTranscriptInsert{Scope: scope, DSHSessionID: id, ParentDSHSessionID: parent}); err != nil {
				return err
			}
		}
		row, found, err := mapper.LockTranscript(ctx, scope, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrDSHHeaderRequired
		}
		result.NextOffset = row.NextOffset
		if events[0].Offset == 0 && !sameDSHParent(row.ParentDSHSessionID, parent) {
			return ErrDSHTranscriptConflict
		}
		if len(events) == 0 {
			return ErrInvalidState
		}
		existing, err := mapper.ReadRecords(ctx, scope, row.UUID, events[0].Offset, len(events))
		if err != nil {
			return err
		}
		return appendDSHRecords(ctx, mapper, scope, row, events, existing, &result)
	})
	return result, err
}

func appendDSHRecords(ctx context.Context, mapper DSHTranscriptMapper, scope DSHScope, transcript dshTranscriptRow, events []DSHTranscriptRecord, existing []dshTranscriptRecordRow, result *DSHTranscriptAppendResult) error {
	stored := make(map[int64][]byte, len(existing))
	for _, event := range existing {
		stored[event.Offset] = event.Payload
	}
	next := transcript.NextOffset
	pending := make([]DSHTranscriptRecord, 0, len(events))
	for index, event := range events {
		if event.Offset < 0 || event.Offset > 9007199254740992 || index > 0 && event.Offset != events[index-1].Offset+1 {
			return ErrInvalidState
		}
		if event.Offset < next {
			old, found := stored[event.Offset]
			if !found {
				return ErrDSHTranscriptConflict
			}
			if !bytes.Equal(old, event.Payload) {
				return ErrDSHTranscriptConflict
			}
			continue
		}
		if event.Offset != next {
			return ErrDSHTranscriptGap
		}
		pending = append(pending, event)
		next++
	}
	if len(pending) > 0 {
		rows, err := mapper.InsertRecords(ctx, dshTranscriptRecordsInsert{Scope: scope, TranscriptUUID: transcript.UUID, Records: pending})
		if err != nil {
			return err
		}
		if rows != int64(len(pending)) {
			return ErrInvalidState
		}
		rows, err = mapper.SetNextOffset(ctx, scope, transcript.UUID, next)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrInvalidState
		}
	}
	result.NextOffset = next
	return nil
}

func (d *DB) ListDSHTranscripts(ctx context.Context, scope DSHScope, after string, limit int) ([]DSHTranscript, error) {
	rows, err := NewDSHTranscriptMapper(d.mapperDB).List(ctx, scope, after, limit)
	if err != nil {
		return nil, err
	}
	result := make([]DSHTranscript, 0, len(rows))
	for _, row := range rows {
		result = append(result, dshTranscriptFromRow(row))
	}
	return result, nil
}

func (d *DB) ReadDSHTranscript(ctx context.Context, scope DSHScope, id string, fromOffset int64, limit int) (DSHTranscript, []DSHTranscriptRecord, error) {
	mapper := NewDSHTranscriptMapper(d.mapperDB)
	row, found, err := mapper.Find(ctx, scope, id)
	if err != nil {
		return DSHTranscript{}, nil, err
	}
	if !found {
		return DSHTranscript{}, nil, ErrNotFound
	}
	rows, err := mapper.ReadRecords(ctx, scope, row.UUID, fromOffset, limit)
	if err != nil {
		return DSHTranscript{}, nil, err
	}
	events := make([]DSHTranscriptRecord, 0, len(rows))
	for _, event := range rows {
		events = append(events, DSHTranscriptRecord{Offset: event.Offset, Payload: event.Payload})
	}
	return dshTranscriptFromRow(row), events, nil
}
