package db

import (
	"testing"

	"github.com/google/uuid"
)

func TestConsoleWorkspaceRowMapsJSONFields(t *testing.T) {
	row := consoleWorkspaceRow{
		UUID:       uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		ExternalID: "workspace_external",
		OrgUUID:    uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		Tags:       []byte(`{"team":"platform"}`),
	}

	workspace, err := row.workspace()
	if err != nil {
		t.Fatalf("workspace(): %v", err)
	}
	if workspace.UUID != "22222222-2222-4222-8222-222222222222" || workspace.ExternalID != "workspace_external" {
		t.Fatalf("workspace identifiers = (%q, %q), want database UUID and external ID", workspace.UUID, workspace.ExternalID)
	}
	if workspace.Tags["team"] != "platform" {
		t.Fatalf("tags = %#v, want platform team", workspace.Tags)
	}
}
