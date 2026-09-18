package db

import (
	"context"
	"time"

	"github.com/superduck-ai/yourbatis"
)

// TranscriptScope carries stable tenant and code session identities, never database identities.
type TranscriptScope struct {
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionUUID       string
	CodeSessionExternalID string
}

// TranscriptArchive is the immutable manifest of one verified transcript object.
type TranscriptArchive struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionUUID       string
	CodeSessionExternalID string
	FromSequence          int64
	ToSequence            int64
	EventCount            int
	Codec                 string
	Bucket                string
	Key                   string
	Size                  int64
	RawBytes              int64
	SHA256                string
	State                 string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (a TranscriptArchive) Scope() TranscriptScope {
	return TranscriptScope{a.OrganizationUUID, a.WorkspaceUUID, a.CodeSessionUUID, a.CodeSessionExternalID}
}

func (d *DB) RegisterTranscriptArchive(ctx context.Context, archive TranscriptArchive) error {
	return d.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		mapper := NewTranscriptArchiveMapper(tx)
		if _, err := mapper.LockScope(ctx, "transcript-archive/"+archive.CodeSessionUUID); err != nil {
			return err
		}
		overlaps, err := mapper.Overlaps(ctx, archive)
		if err != nil {
			return err
		}
		if overlaps {
			return ErrDuplicate
		}
		return mapper.Insert(ctx, archive)
	})
}

func (d *DB) AttachTranscriptArchive(ctx context.Context, scope TranscriptScope, archiveUUID string) error {
	return d.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		rows, err := NewTranscriptArchiveMapper(tx).Attach(ctx, scope, archiveUUID)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrInvalidState
		}
		return nil
	})
}

func (d *DB) FindTranscriptArchive(ctx context.Context, scope TranscriptScope, fromSequence int64) (TranscriptArchive, bool, error) {
	row, found, err := NewTranscriptArchiveMapper(d.mapperDB).FindByRange(ctx, scope, fromSequence)
	return TranscriptArchive(row), found, err
}

func (d *DB) ListTranscriptArchives(ctx context.Context, scope TranscriptScope, after int64, limit int, attachedOnly bool) ([]TranscriptArchive, error) {
	rows, err := NewTranscriptArchiveMapper(d.mapperDB).ListBySession(ctx, scope, after, limit, attachedOnly)
	archives := make([]TranscriptArchive, len(rows))
	for i := range rows {
		archives[i] = TranscriptArchive(rows[i])
	}
	return archives, err
}

// ScheduleTranscriptArchiveCleanup never claims attached segments, which may be the only remaining copy.
func (d *DB) ScheduleTranscriptArchiveCleanup(ctx context.Context, limit int) error {
	return d.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		rows, err := NewTranscriptArchiveMapper(tx).ClaimCleanup(ctx, limit)
		if err != nil {
			return err
		}
		for _, row := range rows {
			payload, err := objectCleanupJobPayload(row.Bucket, row.Key, "transcript_archive", row.ExternalID)
			if err != nil {
				return err
			}
			if err := NewObjectCleanupJobMapper(tx).EnqueueObjectCleanupJob(ctx, row.WorkspaceUUID, payload); err != nil {
				return err
			}
		}
		return nil
	})
}

// TranscriptArchiveQuery freezes age eligibility for a sweep; terminal and boundary are separate modes.
type TranscriptArchiveQuery struct {
	Scope         TranscriptScope
	AfterUUID     string
	AfterSequence int64
	ToSequence    int64
	Limit         int
	Terminal      bool
	Cutoff        time.Time
}

func (d *DB) ListArchivableTranscriptSessions(ctx context.Context, query TranscriptArchiveQuery) ([]TranscriptScope, error) {
	rows, err := NewCodeSessionInternalEventMapper(d.mapperDB).ListArchiveCandidates(ctx, query)
	scopes := make([]TranscriptScope, len(rows))
	for i := range rows {
		scopes[i] = TranscriptScope(rows[i])
	}
	return scopes, err
}

func (d *DB) ListArchivableInternalEvents(ctx context.Context, query TranscriptArchiveQuery) ([]CodeSessionInternalEvent, error) {
	rows, err := NewCodeSessionInternalEventMapper(d.mapperDB).ListArchivable(ctx, query)
	return codeSessionInternalEvents(rows), err
}

// TranscriptDeleteBatch contains ONLY sequences read back from a verified attached object.
// A sparse segment range is not evidence that every row in that range was archived.
type TranscriptDeleteBatch struct {
	Scope       TranscriptScope
	ArchiveUUID string
	Sequences   []int64
	Cutoff      time.Time
}

func (d *DB) SoftDeleteInternalEventsBatch(ctx context.Context, batch TranscriptDeleteBatch) (int64, error) {
	return d.deleteTranscriptBatch(ctx, batch, false)
}

func (d *DB) HardDeleteInternalEventsBatch(ctx context.Context, batch TranscriptDeleteBatch) (int64, error) {
	return d.deleteTranscriptBatch(ctx, batch, true)
}

func (d *DB) deleteTranscriptBatch(ctx context.Context, batch TranscriptDeleteBatch, hard bool) (int64, error) {
	if len(batch.Sequences) == 0 || len(batch.Sequences) > 500 {
		return 0, ErrLimitExceeded
	}
	var count int64
	err := d.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		archives := NewTranscriptArchiveMapper(tx)
		_, found, err := archives.LockAttached(ctx, batch.Scope, batch.ArchiveUUID)
		if err != nil {
			return err
		}
		if !found {
			return ErrInvalidState
		}
		for _, sequence := range batch.Sequences {
			covered, err := archives.HasAttachedCovering(ctx, batch.Scope, batch.ArchiveUUID, sequence)
			if err != nil {
				return err
			}
			if !covered {
				return ErrInvalidState
			}
		}
		mapper := NewCodeSessionInternalEventMapper(tx)
		if hard {
			count, err = mapper.HardDeleteArchived(ctx, batch)
		} else {
			count, err = mapper.SoftDeleteArchived(ctx, batch)
		}
		return err
	})
	return count, err
}

func (d *DB) ReadTranscriptArchiveRange(ctx context.Context, query TranscriptArchiveQuery) ([]CodeSessionInternalEvent, error) {
	rows, err := NewCodeSessionInternalEventMapper(d.mapperDB).ReadArchiveRange(ctx, query)
	return codeSessionInternalEvents(rows), err
}
