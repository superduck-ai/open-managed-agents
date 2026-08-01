package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/samber/lo"
)

type SessionEventDelivery string

const (
	SessionEventDeliveryRealtime      SessionEventDelivery = "realtime"
	SessionEventDeliveryStartupQueued SessionEventDelivery = "startup_queued"
)

// SessionEventQueueItem couples one temporary queue identity with its owned
// public Session event.
type SessionEventQueueItem struct {
	id               int64
	sessionUUID      uuid.UUID
	sessionEventUUID uuid.UUID
	Event            SessionEvent
}

type sessionEventQueueIdentityRow struct {
	ID               int64     `db:"id"`
	SessionUUID      uuid.UUID `db:"session_uuid"`
	SessionEventUUID uuid.UUID `db:"session_event_uuid"`
}

type sessionEventQueueInsertRow struct {
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID    uuid.UUID `db:"workspace_uuid"`
	SessionUUID      uuid.UUID `db:"session_uuid"`
	SessionEventUUID uuid.UUID `db:"session_event_uuid"`
}

// AppendSessionEventsForDelivery keeps the existing delivery path outside the
// managed-agent startup window. During startup it accepts exactly one
// user.message with an empty queue and records the public event and temporary
// delivery responsibility in the same transaction.
func (d *DB) AppendSessionEventsForDelivery(
	ctx context.Context,
	workspaceUUID string,
	sessionExternalID string,
	events []SessionEvent,
	outcomeEvaluations json.RawMessage,
) ([]SessionEvent, SessionEventDelivery, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()

	session, err := getSessionSQLX(
		ctx,
		tx,
		lockSessionForEventsQuery,
		sessionLookupArguments(workspaceUUID, sessionExternalID),
	)
	if err != nil {
		return nil, "", err
	}
	if session.ArchivedAt != nil {
		return nil, "", ErrSessionArchived
	}
	userMessageCount := lo.CountBy(events, func(event SessionEvent) bool {
		return event.EventType == "user.message"
	})
	shouldEnqueue := false
	if userMessageCount > 0 {
		shouldEnqueue, err = shouldQueueForStartup(ctx, tx, session)
		if err != nil {
			return nil, "", err
		}
	}
	if shouldEnqueue {
		if len(events) != 1 || userMessageCount != 1 {
			return nil, "", ErrSessionStartupMessageConflict
		}
		hasQueuedEvents, err := sessionEventQueueExists(ctx, tx, session)
		if err != nil {
			return nil, "", err
		}
		if hasQueuedEvents {
			return nil, "", ErrSessionStartupMessageConflict
		}
	}

	created, err := insertSessionEventsSQLXTx(ctx, tx, session, events, false)
	if err != nil {
		return nil, "", err
	}
	delivery := SessionEventDeliveryRealtime
	if shouldEnqueue {
		if err := enqueueSessionEventsTx(ctx, tx, session, created); err != nil {
			return nil, "", err
		}
		delivery = SessionEventDeliveryStartupQueued
	}
	if len(outcomeEvaluations) > 0 {
		if _, err := getSessionSQLX(ctx, tx, setSessionOutcomeEvaluationsQuery, map[string]any{
			"workspace_uuid":      dbUUID(session.WorkspaceUUID),
			"session_external_id": session.ExternalID,
			"outcome_evaluations": jsonArg(outcomeEvaluations),
		}); err != nil {
			return nil, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, "", err
	}
	return created, delivery, nil
}

// ListSessionEventQueueItems returns the current startup queue in FIFO order
// and rejects references that do not belong to the supplied Session.
func (d *DB) ListSessionEventQueueItems(
	ctx context.Context,
	session Session,
) ([]SessionEventQueueItem, error) {
	identityRows, err := listSessionEventQueueIdentityRows(ctx, d.sql, session, false)
	if err != nil {
		return nil, err
	}
	if len(identityRows) == 0 {
		return nil, nil
	}

	eventsByUUID, err := sessionEventsByUUIDs(ctx, d.sql, session, identityRows)
	if err != nil {
		return nil, err
	}

	queueItems := make([]SessionEventQueueItem, 0, len(identityRows))
	for _, row := range identityRows {
		event, ok := eventsByUUID[row.SessionEventUUID.String()]
		if !ok {
			return nil, fmt.Errorf(
				"%w: queued event %s does not belong to Session %s",
				ErrInvalidState,
				row.SessionEventUUID.String(),
				session.ExternalID,
			)
		}
		queueItems = append(queueItems, SessionEventQueueItem{
			id:               row.ID,
			sessionUUID:      row.SessionUUID,
			sessionEventUUID: row.SessionEventUUID,
			Event:            event,
		})
	}
	return queueItems, nil
}

func sessionEventsByUUIDs(
	ctx context.Context,
	database sqlxNamedQueryer,
	session Session,
	identityRows []sessionEventQueueIdentityRow,
) (map[string]SessionEvent, error) {
	eventUUIDs := make([]string, len(identityRows))
	for i, row := range identityRows {
		eventUUIDs[i] = row.SessionEventUUID.String()
	}
	events, err := listSessionEventsSQLX(ctx, database, `
		select `+sessionEventSQLXColumns+`
		from session_events
		where uuid = any(:session_event_uuids)
			and session_uuid = :session_uuid
			and deleted_at is null
	`, map[string]any{
		"session_event_uuids": eventUUIDs,
		"session_uuid":        dbUUID(session.UUID),
	})
	if err != nil {
		return nil, err
	}
	byUUID := make(map[string]SessionEvent, len(events))
	for _, event := range events {
		byUUID[event.UUID] = event
	}
	return byUUID, nil
}

func listSessionEventQueueIdentityRows(
	ctx context.Context,
	database sqlxNamedQueryer,
	session Session,
	lock bool,
) ([]sessionEventQueueIdentityRow, error) {
	// session_uuid uniquely identifies the public Session; tenant columns are
	// written on insert but are not required as query predicates.
	query := `
		select q.id, q.session_uuid, q.session_event_uuid
		from session_event_queue q
		where q.session_uuid = :session_uuid
		order by q.id asc
	`
	if lock {
		query += ` for update of q`
	}
	var rows []sessionEventQueueIdentityRow
	err := namedSelectContext(ctx, database, &rows, query, map[string]any{
		"session_uuid": dbUUID(session.UUID),
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func queueItemsMatch(
	rows []sessionEventQueueIdentityRow,
	queueItems []SessionEventQueueItem,
) bool {
	if len(rows) != len(queueItems) {
		return false
	}
	for i := range rows {
		if rows[i].ID != queueItems[i].id ||
			rows[i].SessionUUID != queueItems[i].sessionUUID ||
			rows[i].SessionEventUUID != queueItems[i].sessionEventUUID {
			return false
		}
	}
	return true
}

// QueueMatches reports whether the locked startup queue still matches the
// caller's queueItems snapshot (count, order, and identity fields).
func (tx ManagedAgentActivationTx) QueueMatches(
	ctx context.Context,
	session Session,
	queueItems []SessionEventQueueItem,
) (bool, error) {
	rows, err := listSessionEventQueueIdentityRows(ctx, tx.tx, session, true)
	if err != nil {
		return false, err
	}
	return queueItemsMatch(rows, queueItems), nil
}

func (tx ManagedAgentActivationTx) DeleteSessionEventQueue(
	ctx context.Context,
	sessionUUID string,
) error {
	_, err := namedExecRowsAffected(ctx, tx.tx, deleteSessionEventQueueQuery, map[string]any{
		"session_uuid": dbUUID(sessionUUID),
	})
	return err
}

// shouldQueueForStartup reports whether a user.message should enter the startup
// queue. Environment type is intentionally not part of this decision: queueing
// depends only on whether the Session still has no active Code Session and still
// has session-scoped environment work in flight.
func shouldQueueForStartup(
	ctx context.Context,
	database sqlxNamedQueryer,
	session Session,
) (bool, error) {
	var status string
	err := namedGetContext(ctx, database, &status, `
		select status
		from code_sessions
		where session_uuid = :session_uuid
			and deleted_at is null
		order by created_at desc, uuid desc
		limit 1
	`, map[string]any{
		"session_uuid": dbUUID(session.UUID),
	})
	if err == nil && status != "initializing" {
		return false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var startupWorkExists bool
	err = namedGetContext(ctx, database, &startupWorkExists, `
		select exists (
			select 1
			from environment_work ew
			where ew.workspace_uuid = :workspace_uuid
				and ew.environment_uuid = :environment_uuid
				and ew.data->>'type' = 'session'
				and ew.data->>'id' = :session_external_id
				and ew.state in ('queued', 'starting', 'active')
				and ew.deleted_at is null
		)
	`, map[string]any{
		"workspace_uuid":      dbUUID(session.WorkspaceUUID),
		"environment_uuid":    dbUUID(session.EnvironmentUUID),
		"session_external_id": session.ExternalID,
	})
	if err != nil {
		return false, err
	}
	return startupWorkExists, nil
}

func sessionEventQueueExists(
	ctx context.Context,
	database sqlxNamedQueryer,
	session Session,
) (bool, error) {
	var exists bool
	err := namedGetContext(ctx, database, &exists, `
		select exists (
			select 1
			from session_event_queue q
			where q.session_uuid = :session_uuid
		)
	`, map[string]any{
		"session_uuid": dbUUID(session.UUID),
	})
	return exists, err
}

func enqueueSessionEventsTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	events []SessionEvent,
) error {
	organizationUUID, err := parseDBUUID("organization_uuid", session.OrganizationUUID)
	if err != nil {
		return err
	}
	workspaceUUID, err := parseDBUUID("workspace_uuid", session.WorkspaceUUID)
	if err != nil {
		return err
	}
	sessionUUID, err := parseDBUUID("session_uuid", session.UUID)
	if err != nil {
		return err
	}

	rows := make([]sessionEventQueueInsertRow, 0, len(events))
	for _, event := range events {
		if event.EventType != "user.message" {
			continue
		}
		eventUUID, err := parseDBUUID("session_event_uuid", event.UUID)
		if err != nil {
			return err
		}
		rows = append(rows, sessionEventQueueInsertRow{
			OrganizationUUID: organizationUUID,
			WorkspaceUUID:    workspaceUUID,
			SessionUUID:      sessionUUID,
			SessionEventUUID: eventUUID,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	_, err = tx.NamedExecContext(ctx, `
		insert into session_event_queue (
			organization_uuid, workspace_uuid, session_uuid, session_event_uuid
		)
		values (
			:organization_uuid,
			:workspace_uuid,
			:session_uuid,
			:session_event_uuid
		)
	`, rows)
	return err
}
