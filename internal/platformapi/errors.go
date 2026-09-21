package platformapi

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func organizationAccessDenied(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "organization access denied"})
}

func workspaceAccessDenied(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "workspace not allowed"})
}

func defaultWorkspaceNameReserved(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Default Workspace name is reserved"})
}

func workspaceAlreadyExists(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "Workspace already exists"})
}

func invalidEmailLoginRequest(cause error) error {
	return apperr.New(apperr.InvalidArgument, "Invalid email login request", cause)
}
