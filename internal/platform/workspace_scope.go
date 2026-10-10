package platform

import (
	"strings"
)

const DefaultWorkspaceDisplayID = "default"

type WorkspaceScope struct {
	UUID      string
	DisplayID string
}

func ResolveWorkspaceScope(reference string, workspaces []ConsoleWorkspace) (WorkspaceScope, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return WorkspaceScope{}, ErrNotFound
	}
	if reference == DefaultWorkspaceDisplayID {
		return resolveDefaultWorkspaceScope(workspaces)
	}
	for _, workspace := range workspaces {
		if workspace.ArchivedAt != nil {
			continue
		}
		if reference == strings.TrimSpace(workspace.UUID) || reference == strings.TrimSpace(workspace.ExternalID) {
			return workspaceScope(workspace, workspace.ExternalID), nil
		}
	}
	return WorkspaceScope{}, ErrNotFound
}

func resolveDefaultWorkspaceScope(workspaces []ConsoleWorkspace) (WorkspaceScope, error) {
	for _, workspace := range workspaces {
		if workspace.IsDefault && workspace.ArchivedAt == nil {
			return workspaceScope(workspace, DefaultWorkspaceDisplayID), nil
		}
	}
	return WorkspaceScope{}, ErrNotFound
}

func workspaceScope(workspace ConsoleWorkspace, displayID string) WorkspaceScope {
	displayID = strings.TrimSpace(displayID)
	if displayID == "" {
		displayID = strings.TrimSpace(workspace.UUID)
	}
	return WorkspaceScope{
		UUID:      strings.TrimSpace(workspace.UUID),
		DisplayID: displayID,
	}
}
