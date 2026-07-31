package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

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
	sessionUUID      string
	sessionEventUUID string
	Event            SessionEvent
}

type sessionEventQueueIdentityRow struct {
	ID               int64  `db:"id"`
	SessionUUID      string `db:"session_uuid"`
	SessionEventUUID string `db:"session_event_uuid"`
}

// AppendSessionEventsForDelivery keeps the existing delivery path outside the
// managed-agent startup window. During startup it accepts exactly one
// user.message with an empty queue and records the public event and temporary
// delivery responsibility in the same transaction.
func (d *DB) AppendSessionEventsForDelivery(
	ctx context.Context,
	workspaceID int64,
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
		sessionLookupArguments(workspaceID, sessionExternalID),
	)
	if err != nil {
		return nil, "", err
	}
	if session.ArchivedAt != nil {
		return nil, "", ErrInvalidState
	}
	userMessageCount := lo.CountBy(events, func(event SessionEvent) bool {
		return event.EventType == "user.message"
	})
	startup := false
	if userMessageCount > 0 {
		startup, err = sessionUserMessageStartupWindowSQLX(ctx, tx, session)
		if err != nil {
			return nil, "", err
		}
	}
	if startup {
		if len(events) != 1 || userMessageCount != 1 {
			return nil, "", ErrSessionStartupMessageConflict
		}
		pending, err := sessionEventQueueExistsSQLX(ctx, tx, session)
		if err != nil {
			return nil, "", err
		}
		if pending {
			return nil, "", ErrSessionStartupMessageConflict
		}
	}

	created, err := insertSessionEventsSQLXTx(ctx, tx, session, events, false)
	if err != nil {
		return nil, "", err
	}
	delivery := SessionEventDeliveryRealtime
	if startup {
		if err := enqueueSessionEventsSQLXTx(ctx, tx, session, created); err != nil {
			return nil, "", err
		}
		delivery = SessionEventDeliveryStartupQueued
	}
	if len(outcomeEvaluations) > 0 {
		if _, err := getSessionSQLX(ctx, tx, setSessionOutcomeEvaluationsQuery, map[string]any{
			"workspace_id":        session.WorkspaceID,
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
			where organization_id = :organization_id
				and workspace_id = :workspace_id
				and uuid = CAST(:session_event_uuid AS uuid)
				and session_id = :session_id
				and session_external_id = :session_external_id
				and deleted_at is null
		`, map[string]any{
			"organization_id":     session.OrganizationID,
			"workspace_id":        session.WorkspaceID,
			"session_event_uuid":  row.SessionEventUUID,
			"session_id":          session.ID,
			"session_external_id": session.ExternalID,
		})
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf(
				"%w: queued event %s does not belong to Session %s",
				ErrInvalidState,
				row.SessionEventUUID,
				session.ExternalID,
			)
		}
		if err != nil {
			return nil, err
		}
		items = append(items, SessionEventQueueItem{
			id:               row.ID,
			sessionUUID:      row.SessionUUID,
			sessionEventUUID: row.SessionEventUUID,
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
	query := `
		select q.id, CAST(q.session_uuid AS text) as session_uuid,
			CAST(q.session_event_uuid AS text) as session_event_uuid
		from session_event_queue q
		join organizations o on o.uuid = q.organization_uuid
		join workspaces w on w.uuid = q.workspace_uuid and w.organization_id = o.id
		where o.id = :organization_id
			and w.id = :workspace_id
			and q.session_uuid = CAST(:session_uuid AS uuid)
		order by q.id asc
	`
	if lock {
		query += ` for update of q`
	}
	var rows []sessionEventQueueIdentityRow
	err := namedSelectContext(ctx, database, &rows, query, map[string]any{
		"organization_id": session.OrganizationID,
		"workspace_id":    session.WorkspaceID,
		"session_uuid":    session.UUID,
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
			rows[i].SessionUUID != items[i].sessionUUID ||
			rows[i].SessionEventUUID != items[i].sessionEventUUID {
			return false
		}
	}
	return true
}

func sessionUserMessageStartupWindowSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	session Session,
) (bool, error) {
	var status string
	err := namedGetContext(ctx, database, &status, `
		select status
		from code_sessions
		where organization_id = :organization_id
			and workspace_id = :workspace_id
			and session_id = :session_id
			and deleted_at is null
		order by created_at desc, id desc
		limit 1
	`, map[string]any{
		"organization_id": session.OrganizationID,
		"workspace_id":    session.WorkspaceID,
		"session_id":      session.ID,
	})
	if err == nil && status != "initializing" {
		return false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	// Only cloud environments run managed Code Session activation that drains
	// the queue (see Runner.prepareManagedAgentLaunch / cloudEnvironment).
	var startupWorkExists bool
	err = namedGetContext(ctx, database, &startupWorkExists, `
		select exists (
			select 1
			from environment_work ew
			join environments e
				on e.id = ew.environment_id
				and e.organization_id = ew.organization_id
				and e.workspace_id = ew.workspace_id
				and e.external_id = ew.environment_external_id
			where ew.organization_id = :organization_id
				and ew.workspace_id = :workspace_id
				and ew.environment_id = :environment_id
				and ew.environment_external_id = :environment_external_id
				and ew.data->>'type' = 'session'
				and ew.data->>'id' = :session_external_id
				and ew.state in ('queued', 'starting', 'active')
				and ew.deleted_at is null
				and e.deleted_at is null
				and e.config->>'type' = 'cloud'
		)
	`, map[string]any{
		"organization_id":         session.OrganizationID,
		"workspace_id":            session.WorkspaceID,
		"environment_id":          session.EnvironmentID,
		"environment_external_id": session.EnvironmentExternalID,
		"session_external_id":     session.ExternalID,
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
			join organizations o on o.uuid = q.organization_uuid
			join workspaces w on w.uuid = q.workspace_uuid and w.organization_id = o.id
			where o.id = :organization_id
				and w.id = :workspace_id
				and q.session_uuid = CAST(:session_uuid AS uuid)
		)
	`, map[string]any{
		"organization_id": session.OrganizationID,
		"workspace_id":    session.WorkspaceID,
		"session_uuid":    session.UUID,
	})
	return exists, err
}

func enqueueSessionEventsSQLXTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	events []SessionEvent,
) error {
	for _, event := range lo.Filter(events, func(event SessionEvent, _ int) bool {
		return event.EventType == "user.message"
	}) {
		result, err := namedExecContext(ctx, tx, `
			insert into session_event_queue (
				organization_uuid, workspace_uuid, session_uuid, session_event_uuid
			)
			select o.uuid, w.uuid,
				CAST(:session_uuid AS uuid), CAST(:session_event_uuid AS uuid)
			from organizations o
			join workspaces w on w.organization_id = o.id
			where o.id = :organization_id and w.id = :workspace_id
		`, map[string]any{
			"organization_id":    session.OrganizationID,
			"workspace_id":       session.WorkspaceID,
			"session_uuid":       session.UUID,
			"session_event_uuid": event.UUID,
		})
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted != 1 {
			return ErrInvalidState
		}
	}
	return nil
}
