package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestConsoleWorkspaceMapperBuilders(t *testing.T) {
	orgUUID := "11111111-1111-4111-8111-111111111111"
	params := upsertConsoleWorkspaceParams{
		UUID:         "22222222-2222-4222-8222-222222222222",
		ExternalID:   "workspace_external",
		OrgUUID:      orgUUID,
		Name:         "Console Workspace",
		DisplayColor: "#9B87F5",
	}

	assertMapperBuilderContract(t, mapperBuilderContract{
		statement:         consoleWorkspaceMapperUpsertStatement,
		bound:             buildConsoleWorkspaceMapperUpsert(yourbatis.DialectPostgres, params),
		wantID:            "ConsoleWorkspaceMapper.Upsert",
		wantKind:          yourbatis.StatementInsert,
		wantArgumentNames: []string{"params.OrgUUID", "params.UUID", "params.ExternalID", "params.Name", "params.ExternalID", "params.DisplayColor"},
		wantSQLFragments:  []string{"WITH org AS", "INSERT INTO workspaces", "ON CONFLICT", "RETURNING"},
	})

	t.Run("active only", func(t *testing.T) {
		bound := buildConsoleWorkspaceMapperList(yourbatis.DialectPostgres, orgUUID, false)
		assertMapperBuilderContract(t, mapperBuilderContract{
			statement:         consoleWorkspaceMapperListStatement,
			bound:             bound,
			wantID:            "ConsoleWorkspaceMapper.List",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"orgUUID"},
			wantSQLFragments:  []string{"organization_uuid = $1", "archived_at IS NULL", "ORDER BY name ASC"},
		})
	})

	t.Run("including archived", func(t *testing.T) {
		bound := buildConsoleWorkspaceMapperList(yourbatis.DialectPostgres, orgUUID, true)
		if strings.Contains(bound.SQL, "archived_at IS NULL") {
			t.Fatalf("generated SQL unexpectedly filters archived rows: %q", bound.SQL)
		}
	})
}

func TestConsoleWorkspaceRowMapsJSONFields(t *testing.T) {
	row := consoleWorkspaceRow{
		UUID:       "22222222-2222-4222-8222-222222222222",
		ExternalID: "workspace_external",
		OrgUUID:    "11111111-1111-4111-8111-111111111111",
		Tags:       []byte(`{"team":"platform"}`),
	}

	workspace, err := row.workspace()
	if err != nil {
		t.Fatalf("workspace(): %v", err)
	}
	if workspace.UUID != row.UUID || workspace.ExternalID != row.ExternalID {
		t.Fatalf("workspace identifiers = (%q, %q)", workspace.UUID, workspace.ExternalID)
	}
	if workspace.Tags["team"] != "platform" {
		t.Fatalf("tags = %#v, want platform team", workspace.Tags)
	}
}
