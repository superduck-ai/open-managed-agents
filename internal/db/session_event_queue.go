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

type SessionArchivedError struct{}

func (*SessionArchivedError) Error() string {
	return "session is archived"
}

type SessionStartupMessageConflictError struct{}

func (*SessionStartupMessageConflictError) Error() string {
	return "session startup message conflict"
}

// SessionEventQueueItem couples one temporary queue identity with its owned
// public Session event.
type SessionEventQueueItem struct {
	id               int64
	sessionUUID      string
	sessionEventUUID string
	Event            SessionEvent
}

type sessionEventQueueIdentityRow struct {
	ID               int64     `db:"id"`
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
		return nil, "", &SessionArchivedError{}
	}
	userMessageCount := lo.CountBy(events, func(event SessionEvent) bool {
		return event.EventType == "user.message"
	})
	shouldQueueForStartup := false
	if userMessageCount > 0 {
		shouldQueueForStartup, err = shouldQueueUserMessageForStartupSQLX(ctx, tx, session)
		if err != nil {
			return nil, "", err
		}
	}
	if shouldQueueForStartup {
		if len(events) != 1 || userMessageCount != 1 {
			return nil, "", &SessionStartupMessageConflictError{}
		}
		pending, err := sessionEventQueueExistsSQLX(ctx, tx, session)
		if err != nil {
			return nil, "", err
		}
		if pending {
			return nil, "", &SessionStartupMessageConflictError{}
		}
	}

	created, err := insertSessionEventsSQLXTx(ctx, tx, session, events, false)
	if err != nil {
		return nil, "", err
	}
	delivery := SessionEventDeliveryRealtime
	if shouldQueueForStartup {
		if err := enqueueSessionEventsSQLXTx(ctx, tx, session, created); err != nil {
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
	rows, err := listSessionEventQueueIdentityRows(ctx, d.sql, session, false)
	if err != nil {
		return nil, err
	}
	items := make([]SessionEventQueueItem, 0, len(rows))
	for _, row := range rows {
		event, err := getSessionEventSQLX(ctx, d.sql, `
			select `+sessionEventSQLXColumns+`
			from session_events
			where uuid = :session_event_uuid
				and deleted_at is null
		`, map[string]any{
			"session_event_uuid": row.SessionEventUUID,
		})
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf(
				"%w: queued event %s does not belong to Session %s",
				ErrInvalidState,
				row.SessionEventUUID.String(),
				session.ExternalID,
			)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, SessionEventQueueItem{
			id:               row.ID,
			sessionUUID:      row.SessionUUID.String(),
			sessionEventUUID: row.SessionEventUUID.String(),
			Event:            event,
		})
	}
	return items, nil
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

func sessionEventQueueItemsMatch(
	rows []sessionEventQueueIdentityRow,
	items []SessionEventQueueItem,
) bool {
	if len(rows) != len(items) {
		return false
	}
	for i := range rows {
		if rows[i].ID != items[i].id ||
			rows[i].SessionUUID.String() != items[i].sessionUUID ||
			rows[i].SessionEventUUID.String() != items[i].sessionEventUUID {
			return false
		}
	}
	return true
}

func (tx ManagedAgentActivationTx) SessionEventQueueMatches(
	ctx context.Context,
	session Session,
	items []SessionEventQueueItem,
) (bool, error) {
	rows, err := listSessionEventQueueIdentityRows(ctx, tx.tx, session, true)
	if err != nil {
		return false, err
	}
	return sessionEventQueueItemsMatch(rows, items), nil
}

func (tx ManagedAgentActivationTx) DeleteSessionEventQueue(
	ctx context.Context,
	sessionUUID string,
) (int64, error) {
	return namedExecRowsAffected(ctx, tx.tx, deleteSessionEventQueueQuery, map[string]any{
		"session_uuid": dbUUID(sessionUUID),
	})
}

// shouldQueueUserMessageForStartupSQLX reports whether a user.message should
// enter the startup queue. Environment type (for example cloud vs self_hosted)
// is intentionally not part of this decision: queueing depends only on whether
// the Session still has no active Code Session and still has session-scoped
// environment work in flight.
func shouldQueueUserMessageForStartupSQLX(
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

func sessionEventQueueExistsSQLX(
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

func enqueueSessionEventsSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	events []SessionEvent,
) error {
	// todo batch
	for _, event := range lo.Filter(events, func(event SessionEvent, _ int) bool {
		return event.EventType == "user.message"
	}) {
		result, err := namedExecContext(ctx, tx, `
			insert into session_event_queue (
				organization_uuid, workspace_uuid, session_uuid, session_event_uuid
			)
			values (
				:organization_uuid,
				:workspace_uuid,
				:session_uuid,
				:session_event_uuid
			)
		`, map[string]any{
			"organization_uuid":  dbUUID(session.OrganizationUUID),
			"workspace_uuid":     dbUUID(session.WorkspaceUUID),
			"session_uuid":       dbUUID(session.UUID),
			"session_event_uuid": dbUUID(event.UUID),
		})
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted != 1 {
			return fmt.Errorf("enqueue session event: inserted %d rows, want 1", inserted)
		}
	}
	return nil
}
