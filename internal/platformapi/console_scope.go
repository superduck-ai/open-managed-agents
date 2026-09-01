package platformapi

import "net/http"

// ConsoleWorkspaceRequestScope is the organization/workspace scope resolved by
// the shared Console authentication and compatibility boundary.
type ConsoleWorkspaceRequestScope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	WorkspaceID      string
}

// ResolveConsoleWorkspaceRequest reuses the same organization visibility and
// workspace lookup rules as the existing Console resources.
func ResolveConsoleWorkspaceRequest(w http.ResponseWriter, r *http.Request, store OrganizationStore) (ConsoleWorkspaceRequestScope, bool) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return ConsoleWorkspaceRequestScope{}, false
	}
	lister, _ := store.(consoleWorkspaceLister)
	workspace, ok := consoleWorkspaceScopeFromRequest(w, r, lister, orgUUID)
	if !ok {
		return ConsoleWorkspaceRequestScope{}, false
	}
	return ConsoleWorkspaceRequestScope{
		OrganizationUUID: orgUUID,
		WorkspaceUUID:    workspace.UUID,
		WorkspaceID:      workspace.DisplayID,
	}, true
}
