package db

import (
	"context"
	"database/sql"
	"errors"
)

const (
	deleteSessionEventQueueQuery = `
		delete from session_event_queue
		where session_uuid = :session_uuid
	`
	latestCodeSessionStatusForStartupQuery = `
		select status
		from code_sessions
		where session_uuid = :session_uuid
			and deleted_at is null
		order by created_at desc, uuid desc
		limit 1
	`
	startupEnvironmentWorkExistsQuery = `
		select exists (
			select 1
			from environment_work ew
			where ew.workspace_uuid = :workspace_uuid
				and ew.environment_uuid = :environment_uuid
				and ew.data->>'type' = 'session'
				and ew.data->>'id' = :session_external_id
				and ew.state in ('queued', 'starting', 'active')
				and ew.deleted_at is null
		) as exists
	`
	enqueueSessionEventQuery = `
		insert into session_event_queue (
			organization_uuid, workspace_uuid, session_uuid, session_event_uuid
		) values (
			:organization_uuid, :workspace_uuid, :session_uuid, :session_event_uuid
		)
	`
)

// These sqlx helpers are intentionally limited to legacy transactions whose
// other statements have not moved to yourbatis. Keeping the queue operation on
// the same sqlx transaction preserves atomicity without adapting sqlx.Tx into a
// custom yourbatis Executor.
func deleteSessionEventQueueSQLX(
	ctx context.Context,
	database sqlxNamedExecer,
	sessionUUID string,
) error {
	_, err := namedExecContext(ctx, database, deleteSessionEventQueueQuery, map[string]any{
		"session_uuid": dbUUID(sessionUUID),
	})
	return err
}

func shouldQueueForStartupSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	session Session,
) (bool, error) {
	arguments := map[string]any{
		"session_uuid":        dbUUID(session.UUID),
		"workspace_uuid":      dbUUID(session.WorkspaceUUID),
		"environment_uuid":    dbUUID(session.EnvironmentUUID),
		"session_external_id": session.ExternalID,
	}
	var status string
	err := namedGetContext(ctx, database, &status, latestCodeSessionStatusForStartupQuery, arguments)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil && status != "initializing" {
		return false, nil
	}

	var exists bool
	err = namedGetContext(ctx, database, &exists, startupEnvironmentWorkExistsQuery, arguments)
	return exists, err
}

func enqueueSessionEventsSQLXTx(
	ctx context.Context,
	database sqlxNamedExecer,
	session Session,
	events []SessionEvent,
) error {
	for _, event := range events {
		if event.EventType != "user.message" {
			continue
		}
		_, err := namedExecContext(ctx, database, enqueueSessionEventQuery, map[string]any{
			"organization_uuid":  dbUUID(session.OrganizationUUID),
			"workspace_uuid":     dbUUID(session.WorkspaceUUID),
			"session_uuid":       dbUUID(session.UUID),
			"session_event_uuid": dbUUID(event.UUID),
		})
		if err != nil {
			return err
		}
	}
	return nil
}
