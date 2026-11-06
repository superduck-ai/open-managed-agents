package db

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
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
	Header             []byte
	NextSeq            int64
}

type DSHTranscriptEvent struct {
	Seq     int64
	Payload []byte
}

type DSHTranscriptAppendResult struct {
	NextSeq int64
}

func dshTranscriptFromRow(row dshTranscriptRow) DSHTranscript {
	return DSHTranscript{DSHSessionID: row.DSHSessionID, ParentDSHSessionID: row.ParentDSHSessionID,
		SessionThreadID: row.SessionThreadID, Header: row.Header, NextSeq: row.NextSeq}
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

func (d *DB) PutDSHTranscript(ctx context.Context, scope DSHScope, id string, parent *string, header []byte) (DSHTranscript, error) {
	var result DSHTranscript
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(ctx, executor, scope); err != nil {
			return err
		}
		mapper := NewDSHTranscriptMapper(executor)
		if parent != nil {
			_, found, err := mapper.Find(ctx, scope, *parent)
			if err != nil {
				return err
			}
			if !found {
				return ErrNotFound
			}
		}
		if _, err := mapper.Insert(ctx, dshTranscriptInsert{Scope: scope, DSHSessionID: id, ParentDSHSessionID: parent, Header: header}); err != nil {
			return err
		}
		row, found, err := mapper.Find(ctx, scope, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		equal, err := jsoncanonical.Equal(row.Header, header)
		if err != nil {
			return err
		}
		if !equal || !sameDSHParent(row.ParentDSHSessionID, parent) {
			return ErrDSHTranscriptConflict
		}
		result = dshTranscriptFromRow(row)
		return nil
	})
	return result, err
}

func sameDSHParent(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (d *DB) AppendDSHTranscript(ctx context.Context, scope DSHScope, id string, events []DSHTranscriptEvent) (DSHTranscriptAppendResult, error) {
	result := DSHTranscriptAppendResult{}
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		if err := lockDSHWorker(ctx, executor, scope); err != nil {
			return err
		}
		mapper := NewDSHTranscriptMapper(executor)
		row, found, err := mapper.LockTranscript(ctx, scope, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrDSHHeaderRequired
		}
		result.NextSeq = row.NextSeq
		if len(events) == 0 {
			return ErrInvalidState
		}
		existing, err := mapper.ReadEvents(ctx, scope, row.UUID, events[0].Seq, len(events))
		if err != nil {
			return err
		}
		return appendDSHEvents(ctx, mapper, scope, row, events, existing, &result)
	})
	return result, err
}

func appendDSHEvents(ctx context.Context, mapper DSHTranscriptMapper, scope DSHScope, transcript dshTranscriptRow, events []DSHTranscriptEvent, existing []dshTranscriptEventRow, result *DSHTranscriptAppendResult) error {
	stored := make(map[int64][]byte, len(existing))
	for _, event := range existing {
		stored[event.Seq] = event.Payload
	}
	next := transcript.NextSeq
	pending := make([]DSHTranscriptEvent, 0, len(events))
	for index, event := range events {
		if event.Seq < 0 || event.Seq >= 9007199254740991 || index > 0 && event.Seq != events[index-1].Seq+1 {
			return ErrInvalidState
		}
		if event.Seq < next {
			old, found := stored[event.Seq]
			if !found {
				return ErrDSHTranscriptConflict
			}
			equal, err := jsoncanonical.Equal(old, event.Payload)
			if err != nil {
				return err
			}
			if !equal {
				return ErrDSHTranscriptConflict
			}
			continue
		}
		if event.Seq != next {
			return ErrDSHTranscriptGap
		}
		pending = append(pending, event)
		next++
	}
	if len(pending) > 0 {
		rows, err := mapper.InsertEvents(ctx, dshTranscriptEventsInsert{Scope: scope, TranscriptUUID: transcript.UUID, Events: pending})
		if err != nil {
			return err
		}
		if rows != int64(len(pending)) {
			return ErrInvalidState
		}
		rows, err = mapper.SetNextSeq(ctx, scope, transcript.UUID, next)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrInvalidState
		}
	}
	result.NextSeq = next
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

func (d *DB) ReadDSHTranscript(ctx context.Context, scope DSHScope, id string, fromSeq int64, limit int) (DSHTranscript, []DSHTranscriptEvent, error) {
	mapper := NewDSHTranscriptMapper(d.mapperDB)
	row, found, err := mapper.Find(ctx, scope, id)
	if err != nil {
		return DSHTranscript{}, nil, err
	}
	if !found {
		return DSHTranscript{}, nil, ErrNotFound
	}
	rows, err := mapper.ReadEvents(ctx, scope, row.UUID, fromSeq, limit)
	if err != nil {
		return DSHTranscript{}, nil, err
	}
	events := make([]DSHTranscriptEvent, 0, len(rows))
	for _, event := range rows {
		events = append(events, DSHTranscriptEvent{Seq: event.Seq, Payload: event.Payload})
	}
	return dshTranscriptFromRow(row), events, nil
}
