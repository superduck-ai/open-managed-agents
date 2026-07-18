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
	if d == nil || d.Pool == nil || strings.TrimSpace(orgUUID) == "" || strings.TrimSpace(workspaceID) == "" {
		return platform.ConsoleWorkspace{}, ErrNotFound
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	defer tx.Rollback(ctx)

	workspace, err := scanConsoleWorkspace(tx.QueryRow(ctx, `
		update workspaces as w
		   set archived_at = coalesce(w.archived_at, now()),
		       updated_at = now()
		  from organizations o
		 where w.organization_id = o.id
		   and (o.external_id = $1 or o.uuid::text = $1)
		   and w.external_id = $2
		   and lower(coalesce(w.name, '')) <> 'default'
		returning
			w.external_id,
			o.uuid::text,
			w.name,
			w.display_color,
			w.display_color,
			w.data_residency,
			w.external_key_id,
			w.tags,
			w.archived_at,
			w.created_at,
			w.updated_at
	`, orgUUID, workspaceID))
	if err != nil {
		return platform.ConsoleWorkspace{}, err
	}

	if _, err := tx.Exec(ctx, `
		update console_api_keys
		   set archived_at = coalesce(archived_at, now()),
		       updated_at = now()
		 where org_uuid = $1 and workspace_id = $2
	`, orgUUID, workspaceID); err != nil {
		return platform.ConsoleWorkspace{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return platform.ConsoleWorkspace{}, err
	}
	return workspace, nil
}
