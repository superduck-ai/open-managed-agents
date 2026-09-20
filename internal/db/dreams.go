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

func dreamJSONArg(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("[]")
	}
	return raw
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
