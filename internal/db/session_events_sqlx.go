package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	sessionEventSQLXColumns = `id, cast(uuid as text) as uuid, external_id, organization_id,
		workspace_id, session_id, session_external_id, thread_id, thread_external_id,
		event_type, payload, processed_at, created_at, deleted_at`
	lockSessionForEventsQuery = `
		select ` + sessionSQLXColumns + `
		from sessions
		where workspace_id = :workspace_id
			and external_id = :session_external_id
			and deleted_at is null
		for update
	`
	primarySessionThreadQuery = `
		select ` + sessionThreadSQLXColumns + `
		from session_threads
		where workspace_id = :workspace_id
			and session_external_id = :session_external_id
			and parent_thread_id is null
			and deleted_at is null
		order by created_at asc, id asc
		limit 1
	`
	sessionThreadByExternalIDQuery = `
		select ` + sessionThreadSQLXColumns + `
		from session_threads
		where workspace_id = :workspace_id
			and session_external_id = :session_external_id
			and external_id = :thread_external_id
			and deleted_at is null
	`
	createSessionEventStatement = `
		insert into session_events (
			uuid, external_id, organization_id, workspace_id, session_id,
			session_external_id, thread_id, thread_external_id, event_type,
			payload, processed_at, created_at
		)
		values (
			:event_uuid, :event_external_id, :organization_id, :workspace_id,
			:session_id, :session_external_id, :thread_id, :thread_external_id,
			:event_type, CAST(:payload AS jsonb), :processed_at, :created_at
		)
	`
	createSessionEventQuery         = createSessionEventStatement + ` returning ` + sessionEventSQLXColumns
	createSessionEventIfAbsentQuery = createSessionEventStatement + `
		on conflict (workspace_id, external_id) do nothing
		returning ` + sessionEventSQLXColumns
)

type sessionEventRow struct {
	ID                int64      `db:"id"`
	UUID              string     `db:"uuid"`
	ExternalID        string     `db:"external_id"`
	OrganizationID    int64      `db:"organization_id"`
	WorkspaceID       int64      `db:"workspace_id"`
	SessionID         int64      `db:"session_id"`
	SessionExternalID string     `db:"session_external_id"`
	ThreadID          *int64     `db:"thread_id"`
	ThreadExternalID  *string    `db:"thread_external_id"`
	EventType         string     `db:"event_type"`
	Payload           []byte     `db:"payload"`
	ProcessedAt       time.Time  `db:"processed_at"`
	CreatedAt         time.Time  `db:"created_at"`
	DeletedAt         *time.Time `db:"deleted_at"`
}

func insertSessionEventsSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	events []SessionEvent,
	ignoreExisting bool,
) ([]SessionEvent, error) {
	primary, err := getSessionThreadSQLX(ctx, tx, primarySessionThreadQuery, map[string]any{
		"workspace_id":        session.WorkspaceID,
		"session_external_id": session.ExternalID,
	})
	if err != nil {
		return nil, err
	}

	created := make([]SessionEvent, 0, len(events))
	for _, event := range events {
		event.OrganizationID = session.OrganizationID
		event.WorkspaceID = session.WorkspaceID
		event.SessionID = session.ID
		event.SessionExternalID = session.ExternalID
		if event.ThreadExternalID == nil {
			event.ThreadID = &primary.ID
			threadExternalID := primary.ExternalID
			event.ThreadExternalID = &threadExternalID
		} else {
			thread, err := getSessionThreadSQLX(ctx, tx, sessionThreadByExternalIDQuery, map[string]any{
				"workspace_id":        session.WorkspaceID,
				"session_external_id": session.ExternalID,
				"thread_external_id":  *event.ThreadExternalID,
			})
			if err != nil {
				return nil, err
			}
			event.ThreadID = &thread.ID
		}
		inserted, err := insertSessionEventSQLX(ctx, tx, event, ignoreExisting)
		if ignoreExisting && errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		created = append(created, inserted)
	}
	return created, nil
}

func getSessionThreadSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (SessionThread, error) {
	var row sessionThreadRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionThread{}, ErrNotFound
		}
		return SessionThread{}, err
	}
	return row.thread(), nil
}

func insertSessionEventSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	event SessionEvent,
	ignoreExisting bool,
) (SessionEvent, error) {
	var row sessionEventRow
	query := createSessionEventQuery
	if ignoreExisting {
		query = createSessionEventIfAbsentQuery
	}
	err := namedGetContext(ctx, database, &row, query, sessionEventArguments(event))
	if err != nil {
		return SessionEvent{}, err
	}
	return row.event(), nil
}

func sessionEventArguments(event SessionEvent) map[string]any {
	return map[string]any{
		"event_uuid":          event.UUID,
		"event_external_id":   event.ExternalID,
		"organization_id":     event.OrganizationID,
		"workspace_id":        event.WorkspaceID,
		"session_id":          event.SessionID,
		"session_external_id": event.SessionExternalID,
		"thread_id":           event.ThreadID,
		"thread_external_id":  event.ThreadExternalID,
		"event_type":          event.EventType,
		"payload":             jsonArg(event.Payload),
		"processed_at":        event.ProcessedAt,
		"created_at":          event.CreatedAt,
	}
}

func (r sessionEventRow) event() SessionEvent {
	return SessionEvent{
		ID:                r.ID,
		UUID:              r.UUID,
		ExternalID:        r.ExternalID,
		OrganizationID:    r.OrganizationID,
		WorkspaceID:       r.WorkspaceID,
		SessionID:         r.SessionID,
		SessionExternalID: r.SessionExternalID,
		ThreadID:          r.ThreadID,
		ThreadExternalID:  r.ThreadExternalID,
		EventType:         r.EventType,
		Payload:           copyRaw(r.Payload),
		ProcessedAt:       r.ProcessedAt,
		CreatedAt:         r.CreatedAt,
		DeletedAt:         r.DeletedAt,
	}
}
