package api

import (
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
	"net/http"
)

func (s *Server) resolveEnvOAuthWorkspace(w http.ResponseWriter, r *http.Request, principal auth.Principal, workspaceID string) (db.AdminWorkspace, bool) {
	if workspaceID == "" || workspaceID == "default" {
		workspaceID = principal.WorkspaceExternalID
	}
	workspace, access, err := s.workspaceAccess.ResolvePrincipal(r.Context(), principal, workspaceID)
	if err != nil || !access.UseResources() || workspace.ArchivedAt != nil {
		status := http.StatusForbidden
		if err != nil && !errors.Is(err, workspaceaccess.ErrDenied) {
			status = http.StatusInternalServerError
		}
		writeEnvOAuthStartError(w, status, envOAuthErrorInvalidRequest, "")
		return workspace, false
	}
	return workspace, true
}
