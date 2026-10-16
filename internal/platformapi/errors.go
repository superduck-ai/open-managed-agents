package platformapi

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
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

func writeConsoleWorkspaceMemberError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "workspace_member_operation_failed", "Could not update workspace membership."
	switch {
	case errors.Is(err, workspaceaccess.ErrDenied):
		status, code, message = http.StatusForbidden, "workspace_access_denied", "You do not have permission to manage this workspace."
	case errors.Is(err, workspaceaccess.ErrDefaultProtected):
		status, code, message = http.StatusConflict, "default_workspace_protected", "Default Workspace membership is managed at the organization level."
	case errors.Is(err, workspaceaccess.ErrInheritedRole):
		status, code, message = http.StatusConflict, "inherited_workspace_role", "Inherited workspace membership cannot be changed or removed."
	case errors.Is(err, workspaceaccess.ErrInvalidRole):
		status, code, message = http.StatusBadRequest, "invalid_workspace_role", "Workspace role is not assignable."
	case errors.Is(err, db.ErrDuplicate):
		status, code, message = http.StatusConflict, "member_exists", "The member already belongs to this workspace."
	case errors.Is(err, db.ErrNotFound):
		status, code, message = http.StatusNotFound, "member_not_found", "Workspace member not found."
	}
	writeJSON(w, status, struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{Error: code, Message: message})
}

func platformAccessUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "platform_access_unavailable", "message": "Could not load platform access."})
}
