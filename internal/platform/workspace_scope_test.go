package platform

import (
	"errors"
	"testing"
	"time"
)

func TestResolveWorkspaceScopeRejectsUnknownAndArchivedReferences(t *testing.T) {
	archivedAt := time.Now().UTC()
	workspaces := []ConsoleWorkspace{{
		UUID:       "00000000-0000-4000-8000-000000000001",
		ExternalID: "workspace_archived",
		ArchivedAt: &archivedAt,
	}}

	for _, reference := range []string{"", "workspace_missing", "workspace_archived"} {
		if _, err := ResolveWorkspaceScope(reference, workspaces); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ResolveWorkspaceScope(%q) error = %v, want ErrNotFound", reference, err)
		}
	}
}

func TestResolveWorkspaceScopeAcceptsUUIDAndExternalID(t *testing.T) {
	workspace := ConsoleWorkspace{
		UUID:       "00000000-0000-4000-8000-000000000001",
		ExternalID: "workspace_test",
	}

	for _, reference := range []string{workspace.UUID, workspace.ExternalID} {
		scope, err := ResolveWorkspaceScope(reference, []ConsoleWorkspace{workspace})
		if err != nil {
			t.Fatalf("ResolveWorkspaceScope(%q): %v", reference, err)
		}
		if scope.UUID != workspace.UUID || scope.DisplayID != workspace.ExternalID {
			t.Fatalf("scope = %#v, want UUID %q and display ID %q", scope, workspace.UUID, workspace.ExternalID)
		}
	}
}

func TestResolveWorkspaceScopeUsesDefaultCompatibilityPrecedence(t *testing.T) {
	now := time.Now().UTC()
	workspaces := []ConsoleWorkspace{
		{
			UUID:       "00000000-0000-4000-8000-000000000003",
			ExternalID: "workspace_oldest",
			CreatedAt:  now.Add(-2 * time.Hour),
		},
		{
			UUID:       "00000000-0000-4000-8000-000000000002",
			ExternalID: "workspace_named_default",
			Name:       "Default",
			CreatedAt:  now.Add(-time.Hour),
		},
		{
			UUID:       "00000000-0000-4000-8000-000000000001",
			ExternalID: "workspace_default",
			CreatedAt:  now,
		},
	}

	scope, err := ResolveWorkspaceScope(DefaultWorkspaceDisplayID, workspaces)
	if err != nil {
		t.Fatalf("ResolveWorkspaceScope(default): %v", err)
	}
	if scope.UUID != workspaces[2].UUID || scope.DisplayID != DefaultWorkspaceDisplayID {
		t.Fatalf("scope = %#v, want explicit default workspace", scope)
	}
}
