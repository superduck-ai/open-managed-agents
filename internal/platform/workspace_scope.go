package platform

import (
	"strings"
	"time"
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
	var selected *ConsoleWorkspace
	for index := range workspaces {
		candidate := &workspaces[index]
		if candidate.ArchivedAt != nil || strings.TrimSpace(candidate.UUID) == "" {
			continue
		}
		if selected == nil || defaultWorkspaceLess(*candidate, *selected) {
			selected = candidate
		}
	}
	if selected == nil {
		return WorkspaceScope{}, ErrNotFound
	}
	return workspaceScope(*selected, DefaultWorkspaceDisplayID), nil
}

func defaultWorkspaceLess(left ConsoleWorkspace, right ConsoleWorkspace) bool {
	leftRank := defaultWorkspaceRank(left)
	rightRank := defaultWorkspaceRank(right)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return workspaceCreatedBefore(left.CreatedAt, right.CreatedAt)
	}
	return strings.TrimSpace(left.UUID) < strings.TrimSpace(right.UUID)
}

func defaultWorkspaceRank(workspace ConsoleWorkspace) int {
	if strings.TrimSpace(workspace.ExternalID) == "workspace_default" {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(workspace.Name), DefaultWorkspaceDisplayID) {
		return 1
	}
	return 2
}

func workspaceCreatedBefore(left time.Time, right time.Time) bool {
	if left.IsZero() {
		return !right.IsZero()
	}
	if right.IsZero() {
		return false
	}
	return left.Before(right)
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
