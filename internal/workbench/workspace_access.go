package workbench

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func (h *workbenchHandler) authorizeWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
		promptID := strings.TrimSpace(chi.URLParam(r, "promptUuid"))
		if workspaceID == "" && promptID == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !visibleWorkbenchOrg(w, r) {
			return
		}
		principal, _ := auth.PrincipalFromContext(r.Context())
		if workspaceID == "" {
			workspaceID = "default"
		}
		resolver := workspaceaccess.New(h.database)
		if promptID == "" && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/prompts") {
			if _, _, err := resolver.Resolve(r.Context(), principal.OrganizationUUID, principal.UserExternalID, workspaceID); err != nil {
				writeWorkbenchWorkspaceError(w, r, err)
				return
			}
			promptID = workbenchDefaultPromptID
		}
		if promptID != "" {
			record, err := h.store.GetWorkbenchPrompt(r.Context(), workbenchOrgUUID(r), promptID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				writeWorkbenchWorkspaceError(w, r, err)
				return
			}
			if record != nil {
				workspaceID = record.WorkspaceUUID
			}
		}
		workspace, access, err := resolver.Resolve(r.Context(), principal.OrganizationUUID, principal.UserExternalID, workspaceID)
		if err != nil {
			writeWorkbenchWorkspaceError(w, r, err)
			return
		}
		principal.WorkspaceUUID = workspace.UUID
		principal.WorkspaceExternalID = workspace.ExternalID
		principal.WorkspaceAccess = access
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func (h *workbenchHandler) promptInCurrentWorkspace(r *http.Request, promptID string) bool {
	record, stored := h.storedPromptRecord(r, promptID)
	if !stored {
		return true
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	return ok && record.WorkspaceUUID == principal.WorkspaceUUID
}
