package db

import (
	"context"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

// ArchiveConsoleWorkspace soft-deletes a console workspace by setting its
// archived_at timestamp and, in the same transaction, cascading the archive to
// every console API key scoped to that workspace. This mirrors the Anthropic
// workspace semantics where archiving immediately revokes all associated API
// keys. The write is idempotent (coalesce preserves the original archived_at)
// and isolated to the workspace's organization via orgUUID. The organization's
// default workspace (name = "default") is never archivable: the WHERE clause
// excludes it so the invariant holds regardless of which identifier the caller
// supplied (the "default" alias or the workspace's real external_id), and such
// a request surfaces as ErrNotFound. A missing workspace also yields ErrNotFound.
func (d *DB) ArchiveConsoleWorkspace(ctx context.Context, orgUUID, workspaceID string) (platform.ConsoleWorkspace, error) {
	if d == nil || d.sql == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(workspaceID) == "" {
		return platform.ConsoleWorkspace{}, ErrNotFound
	}
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	defer func() { _ = tx.Rollback() }()

	workspace, err := getConsoleWorkspaceSQLX(ctx, tx, `
		update workspaces as w
		set archived_at = coalesce(w.archived_at, now()),
			updated_at = now()
		where w.organization_uuid = :organization_uuid
			and (w.uuid = :workspace_uuid or w.external_id = :workspace_external_id)
			and lower(coalesce(w.name, '')) <> 'default'
		returning
			w.uuid,
			w.external_id,
			w.organization_uuid AS org_uuid,
			w.name,
			w.display_color AS display_color,
			w.display_color AS color,
			w.data_residency,
			w.external_key_id,
			w.tags,
			w.archived_at,
			w.created_at,
			w.updated_at
	`, map[string]any{
		"organization_uuid":     dbUUID(orgUUID),
		"workspace_uuid":        tryParseDBUUIDIdentifier(workspaceID),
		"workspace_external_id": strings.TrimSpace(workspaceID),
	})
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}

	if _, err := namedExecContext(ctx, tx, `
		update console_api_keys
		set status = 'archived',
			archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where organization_uuid = :organization_uuid
			and workspace_uuid = :workspace_uuid
	`, map[string]any{
		"organization_uuid": dbUUID(orgUUID),
		"workspace_uuid":    dbUUID(workspace.UUID),
	}); err != nil {
		return platform.ConsoleWorkspace{}, err
	}

	if _, err := namedExecContext(ctx, tx, `
		update api_keys
		set status = 'archived',
			updated_at = now()
		where status = 'active'
			and workspace_uuid = :workspace_uuid
	`, map[string]any{"workspace_uuid": dbUUID(workspace.UUID)}); err != nil {
		return platform.ConsoleWorkspace{}, err
	}

	if err := tx.Commit(); err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	return workspace, nil
}
