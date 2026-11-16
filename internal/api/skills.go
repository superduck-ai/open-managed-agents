package api

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformapi"
)

func (s *Server) skillConsoleScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, ok := platformapi.ResolveConsoleWorkspaceRequest(w, r, s.db)
		if !ok {
			return
		}
		principal, _ := auth.PrincipalFromContext(r.Context())
		principal, err := s.resolvePlatformWorkspace(r, principal, scope.WorkspaceUUID)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		if !principal.WorkspaceAccess.UseResources() {
			httpapi.WriteError(w, r, httpapi.NewError(403, "permission_error", "Workspace not allowed"))
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}
