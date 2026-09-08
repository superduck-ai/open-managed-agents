package platformapi

import (
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func requireOrganizationAdministrator(w http.ResponseWriter, r *http.Request) bool {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || !principal.WorkspaceAccess.ManageOrganization() {
		organizationAccessDenied(w)
		return false
	}
	return true
}

func accessibleConsoleWorkspaces(r *http.Request, store OrganizationStore, workspaces []ConsoleWorkspace) ([]ConsoleWorkspace, error) {
	accessStore, ok := store.(workspaceaccess.Store)
	if !ok {
		return nil, workspaceaccess.ErrDenied
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return nil, workspaceaccess.ErrDenied
	}
	resolver := workspaceaccess.New(accessStore)
	result := make([]ConsoleWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.ArchivedAt != nil && principal.WorkspaceAccess.ManageOrganization() {
			result = append(result, workspace)
			continue
		}
		_, access, err := resolver.Resolve(r.Context(), principal.OrganizationUUID, principal.UserExternalID, workspace.ExternalID)
		if errors.Is(err, workspaceaccess.ErrDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		workspace.EffectiveRole, workspace.RoleSource = access.Role, access.Source
		result = append(result, workspace)
	}
	return result, nil
}
