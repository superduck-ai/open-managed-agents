package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

const (
	sessionEventSQLXColumns = `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		session_uuid, session_external_id,
		thread_uuid, thread_external_id,
		event_type, payload, processed_at, created_at, deleted_at`
	lockSessionForEventsQuery = `
		select ` + sessionSQLXColumns + `
		from sessions
		where workspace_uuid = :workspace_uuid
			and external_id = :session_external_id
			and deleted_at is null
		for update
	`
	primarySessionThreadQuery = `
		select ` + sessionThreadSQLXColumns + `
		from session_threads
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and parent_thread_uuid is null
			and deleted_at is null
		order by created_at asc, uuid asc
		limit 1
	`
	sessionThreadByExternalIDQuery = `
		select ` + sessionThreadSQLXColumns + `
		from session_threads
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :thread_external_id
			and deleted_at is null
	`
	createSessionEventStatement = `
		insert into session_events (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			session_external_id, thread_uuid, thread_external_id, event_type,
			payload, processed_at, created_at
		)
		values (
			:event_uuid, :event_external_id, :organization_uuid, :workspace_uuid,
			:session_uuid, :session_external_id, :thread_uuid, :thread_external_id,
			:event_type, CAST(:payload AS jsonb), :processed_at, :created_at
		)
	`
	createSessionEventQuery         = createSessionEventStatement + ` returning ` + sessionEventSQLXColumns
	createSessionEventIfAbsentQuery = createSessionEventStatement + `
		on conflict (workspace_uuid, external_id) do nothing
		returning ` + sessionEventSQLXColumns
)

type sessionEventRow struct {
	UUID              uuid.UUID     `db:"uuid"`
	ExternalID        string        `db:"external_id"`
	OrganizationUUID  uuid.UUID     `db:"organization_uuid"`
	WorkspaceUUID     uuid.UUID     `db:"workspace_uuid"`
	SessionUUID       uuid.UUID     `db:"session_uuid"`
	SessionExternalID string        `db:"session_external_id"`
	ThreadUUID        uuid.NullUUID `db:"thread_uuid"`
	ThreadExternalID  *string       `db:"thread_external_id"`
	EventType         string        `db:"event_type"`
	Payload           []byte        `db:"payload"`
	ProcessedAt       time.Time     `db:"processed_at"`
	CreatedAt         time.Time     `db:"created_at"`
	DeletedAt         *time.Time    `db:"deleted_at"`
}

func insertSessionEventsSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	events []SessionEvent,
	ignoreExisting bool,
) ([]SessionEvent, error) {
	primary, err := getSessionThreadSQLX(ctx, tx, primarySessionThreadQuery, map[string]any{
		"workspace_uuid":      dbUUID(session.WorkspaceUUID),
		"session_external_id": session.ExternalID,
	})
	if err != nil {
		return nil, err
	}

	created := make([]SessionEvent, 0, len(events))
	for _, event := range events {
		event.OrganizationUUID = session.OrganizationUUID
		event.WorkspaceUUID = session.WorkspaceUUID
		event.SessionUUID = session.UUID
		event.SessionExternalID = session.ExternalID
		if event.ThreadExternalID == nil {
			event.ThreadUUID = &primary.UUID
			threadExternalID := primary.ExternalID
			event.ThreadExternalID = &threadExternalID
		} else {
			thread, err := getSessionThreadSQLX(ctx, tx, sessionThreadByExternalIDQuery, map[string]any{
				"workspace_uuid":      dbUUID(session.WorkspaceUUID),
				"session_external_id": session.ExternalID,
				"thread_external_id":  *event.ThreadExternalID,
			})
			if err != nil {
				return nil, err
			}
			event.ThreadUUID = &thread.UUID
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

func getSessionEventSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (SessionEvent, error) {
	var row sessionEventRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionEvent{}, ErrNotFound
		}
		return SessionEvent{}, err
	}
	return row.event(), nil
}

func listSessionEventsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]SessionEvent, error) {
	var rows []sessionEventRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	events := make([]SessionEvent, len(rows))
	for index := range rows {
		events[index] = rows[index].event()
	}
	return events, nil
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
		"event_uuid":          dbUUID(event.UUID),
		"event_external_id":   event.ExternalID,
		"organization_uuid":   dbUUID(event.OrganizationUUID),
		"workspace_uuid":      dbUUID(event.WorkspaceUUID),
		"session_uuid":        dbUUID(event.SessionUUID),
		"session_external_id": event.SessionExternalID,
		"thread_uuid":         dbNullableUUID(event.ThreadUUID),
		"thread_external_id":  event.ThreadExternalID,
		"event_type":          event.EventType,
		"payload":             jsonArg(event.Payload),
		"processed_at":        event.ProcessedAt,
		"created_at":          event.CreatedAt,
	}
}

func (r sessionEventRow) event() SessionEvent {
	return SessionEvent{
		UUID:              r.UUID.String(),
		ExternalID:        r.ExternalID,
		OrganizationUUID:  r.OrganizationUUID.String(),
		WorkspaceUUID:     r.WorkspaceUUID.String(),
		SessionUUID:       r.SessionUUID.String(),
		SessionExternalID: r.SessionExternalID,
		ThreadUUID:        nullableUUIDString(r.ThreadUUID),
		ThreadExternalID:  r.ThreadExternalID,
		EventType:         r.EventType,
		Payload:           copyRaw(r.Payload),
		ProcessedAt:       r.ProcessedAt,
		CreatedAt:         r.CreatedAt,
		DeletedAt:         r.DeletedAt,
	}
}
