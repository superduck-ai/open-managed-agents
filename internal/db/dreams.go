package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/superduck-ai/yourbatis"
)

// Dream is a durable consolidation request. M1 deliberately only creates and
// reads pending requests; execution is introduced by the later worker module.
type Dream struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	CreatedByAPIKeyUUID   string
	RuntimeUserUUID       string
	Status                string
	Model                 string
	Instructions          *string
	Inputs                json.RawMessage
	Outputs               json.RawMessage
	Error                 json.RawMessage
	Usage                 json.RawMessage
	OutputMemoryStoreUUID string
	InternalSessionUUID   string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	StartedAt             *time.Time
	EndedAt               *time.Time
	ArchivedAt            *time.Time
	ExecutionState        string
	AttemptCount          int
	NextAttemptAt         *time.Time
	ClaimedByWorkerID     *string
	ClaimExpiresAt        *time.Time
	LastError             *string
}

type DreamPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

// DreamSessionTranscript is the immutable authorization relation between a
// Dream and one source Session. The transcript itself stays virtual: no JSONL
// bytes are copied into object storage.
type DreamSessionTranscript struct {
	UUID                    string
	DreamUUID               string
	WorkspaceUUID           string
	SourceSessionUUID       string
	SourceSessionExternalID string
	Ordinal                 int
	CreatedAt               time.Time
}

var errDreamPreparationLost = errors.New("Dream preparation no longer pending")

type ListDreamsPageParams struct {
	WorkspaceUUID   string
	Limit           int
	Cursor          *DreamPageCursor
	IncludeArchived bool
}

func (d *DB) CreateDream(ctx context.Context, dream Dream) (Dream, error) {
	row, err := NewDreamMapper(d.mapperDB).Insert(ctx, insertDreamParams{
		UUID: dream.UUID, ExternalID: dream.ExternalID, OrganizationUUID: dream.OrganizationUUID,
		WorkspaceUUID: dream.WorkspaceUUID, CreatedByAPIKeyUUID: nullableString(dream.CreatedByAPIKeyUUID), RuntimeUserUUID: nullableString(dream.RuntimeUserUUID),
		Status: dream.Status, Model: dream.Model, Instructions: dream.Instructions, Inputs: dreamJSONArg(dream.Inputs),
		CreatedAt: dream.CreatedAt,
	})
	return dreamFromMapperRow(row, err)
}

func (d *DB) GetDream(ctx context.Context, workspaceUUID, externalID string) (Dream, error) {
	row, err := NewDreamMapper(d.mapperDB).FindByExternalID(ctx, workspaceUUID, externalID)
	return dreamFromMapperRow(row, err)
}

func (d *DB) GetDreamByInternalSessionUUID(ctx context.Context, workspaceUUID, internalSessionUUID string) (Dream, error) {
	row, err := NewDreamMapper(d.mapperDB).FindByInternalSessionUUID(ctx, workspaceUUID, internalSessionUUID)
	return dreamFromMapperRow(row, err)
}

func (d *DB) ListDreamsPage(ctx context.Context, params ListDreamsPageParams) ([]Dream, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	filter := listDreamsParams{WorkspaceUUID: params.WorkspaceUUID, Limit: params.Limit + 1, IncludeArchived: params.IncludeArchived}
	if params.Cursor != nil {
		filter.HasCursor = true
		filter.CursorCreatedAt = params.Cursor.CreatedAt
		filter.CursorUUID = params.Cursor.UUID
	}
	rows, err := NewDreamMapper(d.mapperDB).ListPage(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > params.Limit
	if hasMore {
		rows = rows[:params.Limit]
	}
	dreams := make([]Dream, 0, len(rows))
	for _, row := range rows {
		dream, rowErr := dreamFromMapperRow(row, nil)
		if rowErr != nil {
			return nil, false, rowErr
		}
		dreams = append(dreams, dream)
	}
	return dreams, hasMore, nil
}

// ArchiveDream soft-archives the Dream and its internal Session. The Session
// stays live after the Dream reaches a terminal status so the user can inspect
// the environment; archiving the Dream is what archives that Session. Deleting
// the Session is a separate user action.
func (d *DB) ArchiveDream(ctx context.Context, workspaceUUID, externalID string) (Dream, error) {
	var archived Dream
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, txErr := NewDreamMapper(executor).ArchiveByExternalID(ctx, workspaceUUID, externalID)
		if txErr != nil {
			return txErr
		}
		archived, txErr = dreamFromMapperRow(row, nil)
		if txErr != nil {
			return txErr
		}
		if archived.InternalSessionUUID == "" {
			return nil
		}
		if _, txErr = NewEnvironmentWorkMapper(executor).RequestStopForSession(ctx, archived.WorkspaceUUID, archived.InternalSessionUUID); txErr != nil {
			return txErr
		}
		sessionRow, found, txErr := NewSessionMapper(executor).FindByUUID(ctx, archived.WorkspaceUUID, archived.InternalSessionUUID)
		if txErr != nil || !found {
			return txErr
		}
		session := sessionRow.session()
		if session.ArchivedAt != nil {
			return nil
		}
		if session.Status == "running" || session.Status == "rescheduling" {
			if _, txErr = NewSessionMapper(executor).SetStatus(ctx, session.WorkspaceUUID, session.ExternalID, "idle"); txErr != nil {
				return txErr
			}
		}
		_, txErr = NewSessionMapper(executor).Archive(ctx, session.WorkspaceUUID, session.ExternalID)
		return txErr
	})
	return archived, err
}

// RecordPendingDreamResources persists setup progress without exposing it as a
// public status. Pending remains the contractual state until /dream is queued.
func (d *DB) RecordPendingDreamResources(ctx context.Context, workspaceUUID, externalID, workerID, outputMemoryStoreUUID, outputMemoryStoreID, internalSessionUUID, internalSessionID string, transcripts []DreamSessionTranscript, now time.Time) (Dream, bool, error) {
	var recorded Dream
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		transcriptMapper := NewDreamSessionTranscriptMapper(executor)
		for _, transcript := range transcripts {
			if _, err := transcriptMapper.Insert(ctx, insertDreamSessionTranscriptParams{
				UUID: transcript.UUID, DreamUUID: transcript.DreamUUID, WorkspaceUUID: transcript.WorkspaceUUID,
				SourceSessionUUID: transcript.SourceSessionUUID, SourceSessionExternalID: transcript.SourceSessionExternalID,
				Ordinal: transcript.Ordinal, CreatedAt: transcript.CreatedAt,
			}); err != nil {
				return err
			}
		}
		row, err := NewDreamMapper(executor).RecordPendingResources(ctx, recordDreamResourcesParams{
			WorkspaceUUID: workspaceUUID, ExternalID: externalID, WorkerID: workerID,
			OutputMemoryStoreUUID: outputMemoryStoreUUID, OutputMemoryStoreID: outputMemoryStoreID,
			InternalSessionUUID: internalSessionUUID, InternalSessionID: internalSessionID, Now: now,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return errDreamPreparationLost
		}
		if err != nil {
			return err
		}
		var convertErr error
		recorded, convertErr = dreamFromMapperRow(row, nil)
		return convertErr
	})
	if errors.Is(err, errDreamPreparationLost) {
		return Dream{}, false, nil
	}
	return recorded, err == nil, err
}

func (d *DB) MarkClaimedDreamFailed(ctx context.Context, dream Dream, workerID string, errorJSON json.RawMessage, now time.Time) (Dream, bool, error) {
	var failed Dream
	won := false
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, txErr := NewDreamMapper(executor).MarkClaimedFailed(ctx, markClaimedDreamFailedParams{
			WorkspaceUUID: dream.WorkspaceUUID, ExternalID: dream.ExternalID, WorkerID: workerID,
			Error: dreamJSONArg(errorJSON), Usage: dreamJSONArg(json.RawMessage(`{}`)), Now: now,
		})
		if errors.Is(txErr, sql.ErrNoRows) {
			return nil
		}
		if txErr != nil {
			return txErr
		}
		failed, txErr = dreamFromMapperRow(row, nil)
		if txErr != nil {
			return txErr
		}
		won = true
		return nil
	})
	return failed, won, err
}

func (d *DB) ListDreamsByStatus(ctx context.Context, status string, limit int) ([]Dream, error) {
	rows, err := NewDreamMapper(d.mapperDB).ListByStatus(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	return dreamsFromMapperRows(rows)
}

// ListStoppedDreamsWithActiveSession returns canceled or failed Dreams whose
// internal Session is still running, so the worker can re-deliver the stop
// interrupt that did not reach the Code Session.
func (d *DB) ListStoppedDreamsWithActiveSession(ctx context.Context, limit int) ([]Dream, error) {
	rows, err := NewDreamMapper(d.mapperDB).ListStoppedWithActiveSession(ctx, limit)
	if err != nil {
		return nil, err
	}
	return dreamsFromMapperRows(rows)
}

// ListDreamsAwaitingRuntimeReclaim returns terminal Dreams whose internal
// Session still has unstopped Environment Work or is still running, so the
// running worker can kill leftover sandboxes before the archiver runs.
func (d *DB) ListDreamsAwaitingRuntimeReclaim(ctx context.Context, limit int) ([]Dream, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := NewDreamMapper(d.mapperDB).ListTerminalAwaitingRuntimeReclaim(ctx, limit)
	if err != nil {
		return nil, err
	}
	return dreamsFromMapperRows(rows)
}

func (d *DB) ClaimNextPendingDream(ctx context.Context, workerID string, claimFor time.Duration) (*Dream, error) {
	row, err := NewDreamMapper(d.mapperDB).ClaimNextPending(ctx, claimPendingDreamParams{
		WorkerID: workerID, ClaimExpiresAt: time.Now().UTC().Add(claimFor),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dream, err := dreamFromMapperRow(row, nil)
	return &dream, err
}

func (d *DB) RenewPendingDreamClaim(ctx context.Context, dream Dream, workerID string, claimFor time.Duration) (bool, error) {
	now := time.Now().UTC()
	rows, err := NewDreamMapper(d.mapperDB).RenewPendingClaim(ctx, renewPendingDreamClaimParams{
		WorkspaceUUID: dream.WorkspaceUUID, ExternalID: dream.ExternalID, WorkerID: workerID,
		ClaimExpiresAt: now.Add(claimFor), Now: now,
	})
	return rows == 1, err
}

func (d *DB) SchedulePendingDreamRetry(ctx context.Context, dream Dream, workerID string, nextAttemptAt time.Time, cause error) (bool, error) {
	message := "Dream preparation failed"
	if cause != nil {
		message = cause.Error()
	}
	row, err := NewDreamMapper(d.mapperDB).SchedulePendingRetry(ctx, schedulePendingDreamRetryParams{
		WorkspaceUUID: dream.WorkspaceUUID, ExternalID: dream.ExternalID, WorkerID: workerID,
		NextAttemptAt: nextAttemptAt, LastError: message, Now: time.Now().UTC(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	_, err = dreamFromMapperRow(row, err)
	return err == nil, err
}

func (d *DB) ListDreamsAwaitingInternalSessionArchive(ctx context.Context, limit int) ([]Dream, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := NewDreamMapper(d.mapperDB).ListAwaitingInternalSessionArchive(ctx, limit)
	if err != nil {
		return nil, err
	}
	return dreamsFromMapperRows(rows)
}

// StartDream atomically exposes the Dream as running and appends its stable
// command event. Publishing to the active Code Session happens after commit and
// may be retried from the durable Session event.
func (d *DB) StartDream(ctx context.Context, dream Dream, workerID, outputMemoryStoreID string, session Session, event SessionEvent, now time.Time) (Dream, SessionEvent, bool, error) {
	var started Dream
	var command SessionEvent
	won := false
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, txErr := NewDreamMapper(executor).MarkRunning(ctx, markDreamRunningParams{
			WorkspaceUUID: dream.WorkspaceUUID, ExternalID: dream.ExternalID, WorkerID: workerID,
			OutputMemoryStoreID: outputMemoryStoreID, InternalSessionID: session.ExternalID, Now: now,
		})
		if errors.Is(txErr, sql.ErrNoRows) {
			return nil
		}
		if txErr != nil {
			return txErr
		}
		started, txErr = dreamFromMapperRow(row, nil)
		if txErr != nil {
			return txErr
		}

		sessionRow, found, txErr := NewSessionMapper(executor).LockSessionForEvents(ctx, session.WorkspaceUUID, session.ExternalID)
		if txErr != nil {
			return txErr
		}
		if !found {
			return ErrNotFound
		}
		lockedSession := sessionRow.session()
		if lockedSession.ArchivedAt != nil {
			return ErrInvalidState
		}
		created, txErr := insertSessionEventsTx(ctx, executor, lockedSession, []SessionEvent{event}, true)
		if txErr != nil {
			return txErr
		}
		if len(created) > 0 {
			command = created[0]
		} else {
			existing, loadErr := NewSessionEventMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, event.ExternalID)
			if loadErr != nil {
				return loadErr
			}
			command = existing.event()
		}
		won = true
		return nil
	})
	return started, command, won, err
}

func (d *DB) UpdateRunningDreamUsage(ctx context.Context, workspaceUUID, externalID string, usage json.RawMessage, now time.Time) (Dream, bool, error) {
	row, err := NewDreamMapper(d.mapperDB).UpdateRunningUsage(ctx, updateRunningDreamUsageParams{
		WorkspaceUUID: workspaceUUID, ExternalID: externalID, Usage: dreamJSONArg(usage), Now: now,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Dream{}, false, nil
	}
	value, err := dreamFromMapperRow(row, err)
	return value, err == nil, err
}

func (d *DB) MarkDreamTerminal(ctx context.Context, workspaceUUID, externalID, status string, errorJSON, usage json.RawMessage, now time.Time) (Dream, bool, error) {
	var value Dream
	won := false
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, txErr := NewDreamMapper(executor).MarkTerminal(ctx, markDreamTerminalParams{WorkspaceUUID: workspaceUUID, ExternalID: externalID, Status: status, Error: dreamJSONArg(errorJSON), Usage: dreamJSONArg(usage), Now: now})
		if errors.Is(txErr, sql.ErrNoRows) {
			return nil
		}
		if txErr != nil {
			return txErr
		}
		value, txErr = dreamFromMapperRow(row, nil)
		if txErr != nil {
			return txErr
		}
		won = true
		return nil
	})
	return value, won, err
}

// CancelDream atomically closes the public Dream contract and persists the
// stable interrupt when an internal Session exists. The Session and sandbox
// stay available for inspection until the user archives the Dream.
func (d *DB) CancelDream(ctx context.Context, dream Dream, session *Session, interrupt *SessionEvent, now time.Time) (Dream, *SessionEvent, bool, error) {
	var canceled Dream
	var persistedInterrupt *SessionEvent
	won := false
	usage := dream.Usage
	if len(usage) == 0 {
		usage = json.RawMessage(`{}`)
	}
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		row, txErr := NewDreamMapper(executor).MarkTerminal(ctx, markDreamTerminalParams{
			WorkspaceUUID: dream.WorkspaceUUID, ExternalID: dream.ExternalID, Status: "canceled",
			Error: dreamJSONArg(json.RawMessage(`null`)), Usage: dreamJSONArg(usage), Now: now,
		})
		if errors.Is(txErr, sql.ErrNoRows) {
			return nil
		}
		if txErr != nil {
			return txErr
		}
		canceled, txErr = dreamFromMapperRow(row, nil)
		if txErr != nil {
			return txErr
		}
		won = true
		if session == nil || interrupt == nil {
			return nil
		}
		sessionRow, found, txErr := NewSessionMapper(executor).LockSessionForEvents(ctx, session.WorkspaceUUID, session.ExternalID)
		if txErr != nil {
			return txErr
		}
		if !found || sessionRow.ArchivedAt != nil {
			return nil
		}
		created, txErr := insertSessionEventsTx(ctx, executor, sessionRow.session(), []SessionEvent{*interrupt}, true)
		if txErr != nil {
			return txErr
		}
		if len(created) > 0 {
			value := created[0]
			persistedInterrupt = &value
			return nil
		}
		existing, loadErr := NewSessionEventMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, interrupt.ExternalID)
		if loadErr != nil {
			return loadErr
		}
		value := existing.event()
		persistedInterrupt = &value
		return nil
	})
	return canceled, persistedInterrupt, won, err
}

func (d *DB) CreateDreamSessionTranscripts(ctx context.Context, transcripts []DreamSessionTranscript) error {
	if len(transcripts) == 0 {
		return nil
	}
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewDreamSessionTranscriptMapper(executor)
		for _, transcript := range transcripts {
			if _, err := mapper.Insert(ctx, insertDreamSessionTranscriptParams{
				UUID: transcript.UUID, DreamUUID: transcript.DreamUUID, WorkspaceUUID: transcript.WorkspaceUUID,
				SourceSessionUUID: transcript.SourceSessionUUID, SourceSessionExternalID: transcript.SourceSessionExternalID,
				Ordinal: transcript.Ordinal, CreatedAt: transcript.CreatedAt,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) ListDreamSessionTranscripts(ctx context.Context, workspaceUUID, dreamUUID string) ([]DreamSessionTranscript, error) {
	rows, err := NewDreamSessionTranscriptMapper(d.mapperDB).ListByDreamUUID(ctx, workspaceUUID, dreamUUID)
	if err != nil {
		return nil, err
	}
	transcripts := make([]DreamSessionTranscript, 0, len(rows))
	for _, row := range rows {
		transcripts = append(transcripts, DreamSessionTranscript{
			UUID: row.UUID, DreamUUID: row.DreamUUID, WorkspaceUUID: row.WorkspaceUUID,
			SourceSessionUUID: row.SourceSessionUUID, SourceSessionExternalID: row.SourceSessionExternalID,
			Ordinal: row.Ordinal, CreatedAt: row.CreatedAt,
		})
	}
	return transcripts, nil
}

func dreamJSONArg(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("[]")
	}
	return raw
}

func dreamsFromMapperRows(rows []dreamRow) ([]Dream, error) {
	values := make([]Dream, 0, len(rows))
	for _, row := range rows {
		value, err := dreamFromMapperRow(row, nil)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func dreamFromMapperRow(row dreamRow, err error) (Dream, error) {
	if err != nil {
		return Dream{}, mapNoRows(err)
	}
	return Dream{UUID: row.UUID, ExternalID: row.ExternalID, OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID,
		CreatedByAPIKeyUUID: stringFromNullable(row.CreatedByAPIKeyUUID), RuntimeUserUUID: stringFromNullable(row.RuntimeUserUUID), Status: row.Status, Model: row.Model, Instructions: row.Instructions,
		Inputs: json.RawMessage(row.Inputs), Outputs: json.RawMessage(row.Outputs), Error: json.RawMessage(row.Error), Usage: json.RawMessage(row.Usage),
		OutputMemoryStoreUUID: stringFromNullable(row.OutputMemoryStoreUUID), InternalSessionUUID: stringFromNullable(row.InternalSessionUUID),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, StartedAt: row.StartedAt, EndedAt: row.EndedAt, ArchivedAt: row.ArchivedAt,
		ExecutionState: row.ExecutionState, AttemptCount: row.AttemptCount, NextAttemptAt: row.NextAttemptAt,
		ClaimedByWorkerID: row.ClaimedByWorkerID, ClaimExpiresAt: row.ClaimExpiresAt, LastError: row.LastError}, nil
}
