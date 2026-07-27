package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/samber/lo"
)

const (
	// lockManagedAgentEnvironmentWorkQuery 与 patchManagedAgentWorkMetadataQuery
	// 共用同一条 Work 行锁；启动事务先锁定 Work，再在提交前写入 runtime metadata。
	lockManagedAgentEnvironmentWorkQuery = `
		select ` + environmentWorkSQLXColumns + `
		from environment_work
		where workspace_id = :workspace_id
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
		for update
	`
	patchManagedAgentWorkMetadataQuery = `
		update environment_work
		set metadata = coalesce(metadata, CAST('{}' AS jsonb))
				|| CAST(:preparation_patch AS jsonb)
				|| CAST(:runtime_patch AS jsonb),
			updated_at = now()
		where workspace_id = :workspace_id
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
		returning ` + environmentWorkSQLXColumns + `
	`
	// listManagedAgentSessionEventsQuery 在 lockSessionForEventsQuery 取得行锁后
	// 读取事件快照，保证快照与 Code Session 在同一次提交中固化。
	listManagedAgentSessionEventsQuery = `
		select ` + sessionEventSQLXColumns + `
		from session_events
		where workspace_id = :workspace_id
			and session_external_id = :session_external_id
			and deleted_at is null
		order by created_at asc, id asc
	`
	terminateManagedAgentCodeSessionQuery = `
		update code_sessions
		set status = 'terminated',
			oauth_access_token_hash = null,
			worker_lease_expires_at = null,
			connection_status = 'disconnected',
			updated_at = now()
		where organization_id = :organization_id
			and workspace_id = :workspace_id
			and external_id = :code_session_external_id
			and deleted_at is null
		returning session_external_id
	`
	clearTerminatedManagedAgentSessionMetadataQuery = `
		update sessions
		set metadata = metadata
				- 'claude_code_session_id'
				- 'claude_code_public_session_id'
				- 'claude_code_sdk_url_path'
				- 'runtime',
			updated_at = now()
		where organization_id = :organization_id
			and workspace_id = :workspace_id
			and external_id = :session_external_id
			and metadata ->> 'claude_code_session_id' = :code_session_external_id
			and deleted_at is null
	`
	clearTerminatedManagedAgentWorkMetadataQuery = `
		update environment_work
		set metadata = metadata
				- 'claude_code_session_id'
				- 'claude_code_public_session_id'
				- 'claude_code_sdk_url_path'
				- 'runtime',
			updated_at = now()
		where organization_id = :organization_id
			and workspace_id = :workspace_id
			and metadata ->> 'claude_code_session_id' = :code_session_external_id
			and deleted_at is null
	`
)

// TerminateManagedAgentCodeSession revokes credentials for a launch that did
// not complete and atomically removes Session and Work runtime metadata that
// still points at that Code Session. Repeating the operation is safe.
func (d *DB) TerminateManagedAgentCodeSession(
	ctx context.Context,
	organizationID int64,
	workspaceID int64,
	codeSessionExternalID string,
) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	arguments := map[string]any{
		"organization_id":          organizationID,
		"workspace_id":             workspaceID,
		"code_session_external_id": codeSessionExternalID,
	}
	var sessionExternalID string
	if err := namedGetContext(ctx, tx, &sessionExternalID, terminateManagedAgentCodeSessionQuery, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	arguments["session_external_id"] = sessionExternalID
	if _, err := namedExecContext(ctx, tx, clearTerminatedManagedAgentSessionMetadataQuery, arguments); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, clearTerminatedManagedAgentWorkMetadataQuery, arguments); err != nil {
		return err
	}
	return tx.Commit()
}

func getEnvironmentWorkSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (EnvironmentWork, error) {
	var row environmentWorkRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EnvironmentWork{}, ErrNotFound
		}
		return EnvironmentWork{}, err
	}
	return row.work(), nil
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
	return lo.Map(rows, func(row sessionEventRow, _ int) SessionEvent {
		return row.event()
	}), nil
}
