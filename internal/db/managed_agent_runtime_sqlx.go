package db

import (
	"context"
	"encoding/json"
)

const (
	bindManagedAgentSessionMetadataQuery = `
		update sessions
		set metadata = coalesce(metadata, CAST('{}' AS jsonb))
				|| CAST(:metadata_patch AS jsonb),
			updated_at = now()
		where organization_id = :organization_id
			and workspace_id = :workspace_id
			and external_id = :session_external_id
			and deleted_at is null
	`
	bindManagedAgentWorkMetadataQuery = `
		update environment_work
		set metadata = coalesce(metadata, CAST('{}' AS jsonb))
				|| CAST(:metadata_patch AS jsonb),
			updated_at = now()
		where organization_id = :organization_id
			and workspace_id = :workspace_id
			and environment_id = :environment_id
			and environment_external_id = :environment_external_id
			and external_id = :work_external_id
			and deleted_at is null
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
	`
)

// BindManagedAgentRuntimeMetadata atomically publishes a successfully started
// Code Session to the public Session and its Environment Work.
func (d *DB) BindManagedAgentRuntimeMetadata(
	ctx context.Context,
	session Session,
	work EnvironmentWork,
	sessionPatch json.RawMessage,
	workPatch json.RawMessage,
) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sessionResult, err := namedExecContext(ctx, tx, bindManagedAgentSessionMetadataQuery, map[string]any{
		"organization_id":     session.OrganizationID,
		"workspace_id":        session.WorkspaceID,
		"session_external_id": session.ExternalID,
		"metadata_patch":      string(sessionPatch),
	})
	if err != nil {
		return err
	}
	sessionRows, err := sessionResult.RowsAffected()
	if err != nil {
		return err
	}
	if sessionRows == 0 {
		return ErrNotFound
	}

	workResult, err := namedExecContext(ctx, tx, bindManagedAgentWorkMetadataQuery, map[string]any{
		"organization_id":         work.OrganizationID,
		"workspace_id":            work.WorkspaceID,
		"environment_id":          work.EnvironmentID,
		"environment_external_id": work.EnvironmentExternalID,
		"work_external_id":        work.ExternalID,
		"metadata_patch":          string(workPatch),
	})
	if err != nil {
		return err
	}
	workRows, err := workResult.RowsAffected()
	if err != nil {
		return err
	}
	if workRows == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// TerminateManagedAgentCodeSession revokes credentials for a launch that did
// not complete. Repeating the operation is safe.
func (d *DB) TerminateManagedAgentCodeSession(
	ctx context.Context,
	organizationID int64,
	workspaceID int64,
	codeSessionExternalID string,
) error {
	result, err := namedExecContext(ctx, d.sql, terminateManagedAgentCodeSessionQuery, map[string]any{
		"organization_id":          organizationID,
		"workspace_id":             workspaceID,
		"code_session_external_id": codeSessionExternalID,
	})
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
