package platformapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

type consoleWorkspaceArchiver interface {
	ArchiveConsoleWorkspace(ctx context.Context, orgUUID, workspaceID string) (ConsoleWorkspace, error)
}

func handleArchiveConsoleWorkspace(store OrganizationStore) http.HandlerFunc {
	workspaceArchiver, _ := store.(consoleWorkspaceArchiver)
	workspaceLister, _ := store.(consoleWorkspaceLister)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if workspaceArchiver == nil {
			internalError(w, "failed to archive workspace")
			return
		}
		workspaceID, ok := consoleWorkspaceIDFromRequest(w, r, workspaceLister, orgUUID)
		if !ok {
			return
		}
		// The default workspace (literal "default") is the Anthropic-equivalent
		// fallback workspace and must always remain usable; it cannot be archived.
		if workspaceID == defaultConsoleWorkspaceID {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "cannot_archive_default_workspace",
				"message": "the default workspace cannot be archived",
			})
			return
		}
		// Prevent self-lockout: refuse to archive the workspace the caller's
		// session is bound to. This is the backend counterpart of the front-end
		// guard that disables archiving the active workspace.
		if principal, ok := auth.PrincipalFromContext(r.Context()); ok {
			principalWorkspace := strings.TrimSpace(principal.WorkspaceExternalID)
			if principalWorkspace != "" && strings.EqualFold(principalWorkspace, workspaceID) {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":   "cannot_archive_current_workspace",
					"message": "archive the workspace from a different workspace context",
				})
				return
			}
		}
		workspace, err := workspaceArchiver.ArchiveConsoleWorkspace(r.Context(), orgUUID, workspaceID)
		if err != nil {
			if errors.Is(err, platform.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, map[string]any{"error": "workspace not found"})
				return
			}
			internalError(w, "failed to archive workspace")
			return
		}
		writeJSON(w, http.StatusOK, formatConsoleWorkspace(workspace))
	}
}
