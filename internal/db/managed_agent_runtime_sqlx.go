package db

import (
	"context"
	"database/sql"
	"errors"
)

const (
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
	err = namedGetContext(ctx, tx, &sessionExternalID, terminateManagedAgentCodeSessionQuery, arguments)
	if err != nil {
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
