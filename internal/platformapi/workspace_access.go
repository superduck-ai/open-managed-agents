package platformapi

import (
	"context"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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
	accessStore, ok := store.(interface {
		ListUserWorkspaceRoles(context.Context, string, string) ([]db.WorkspaceRoleFact, error)
	})
	if !ok {
		return nil, workspaceaccess.ErrDenied
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserUUID == "" {
		return nil, workspaceaccess.ErrDenied
	}
	roles := map[string]string{}
	if !principal.WorkspaceAccess.ManageOrganization() {
		facts, err := accessStore.ListUserWorkspaceRoles(r.Context(), principal.OrganizationUUID, principal.UserUUID)
		if err != nil {
			return nil, err
		}
		for _, fact := range facts {
			roles[fact.WorkspaceUUID] = fact.Role
		}
	}
	result := make([]ConsoleWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.OrgUUID != principal.OrganizationUUID {
			continue
		}
		if workspace.ArchivedAt != nil && principal.WorkspaceAccess.ManageOrganization() {
			result = append(result, workspace)
			continue
		}
		if workspace.ArchivedAt != nil {
			continue
		}
		access, err := workspaceaccess.Effective(principal.WorkspaceAccess.OrganizationRole, workspace.IsDefault, roles[workspace.UUID])
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
