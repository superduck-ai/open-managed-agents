package platformapi

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var errConsoleOrganizationAdminRequired = errors.New("organization administrator required")

func validateConsoleOrganizationAdminRole(role string) error {
	if role != "admin" {
		return errConsoleOrganizationAdminRequired
	}
	return nil
}

func writeConsoleMemberError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "member_operation_failed", "Could not update organization membership."
	switch {
	case errors.Is(err, errConsoleOrganizationAdminRequired):
		status, code, message = http.StatusForbidden, "organization_admin_required", "Only organization administrators can manage organization members."
	case errors.Is(err, db.ErrLastOrganizationAdmin):
		status, code, message = http.StatusConflict, "last_organization_admin", "The last organization administrator cannot be removed or demoted."
	case errors.Is(err, db.ErrNotFound):
		status, code, message = http.StatusNotFound, "member_not_found", "Organization member not found."
	}
	writeJSON(w, status, struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{Error: code, Message: message})
}

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
