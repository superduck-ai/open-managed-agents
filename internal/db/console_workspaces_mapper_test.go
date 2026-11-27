package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestConsoleWorkspaceMapperBuilders(t *testing.T) {
	orgUUID := "11111111-1111-4111-8111-111111111111"
	params := insertConsoleWorkspaceParams{
		UUID:          "22222222-2222-4222-8222-222222222222",
		ExternalID:    "workspace_external",
		OrgUUID:       orgUUID,
		Name:          "Console Workspace",
		DisplayColor:  "#9B87F5",
		DataResidency: []byte(`{"workspace_geo":"us"}`),
	}

	t.Run("insert uses org default runtime profile when omitted", func(t *testing.T) {
		assertMapperBuilderContract(t, mapperBuilderContract{
			statement:         consoleWorkspaceMapperInsertStatement,
			bound:             buildConsoleWorkspaceMapperInsert(yourbatis.DialectPostgres, params),
			wantID:            "ConsoleWorkspaceMapper.Insert",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"params.OrgUUID", "params.UUID", "params.ExternalID", "params.Name", "params.ExternalID", "params.DisplayColor", "params.DataResidency"},
			wantSQLFragments:  []string{"WITH org AS", "default_runtime_profile_uuid", "INSERT INTO workspaces", "runtime_profile_uuid", "CAST($7 AS jsonb)", "org.default_runtime_profile_uuid", "RETURNING"},
		})
	})

	t.Run("insert binds explicit runtime profile when provided", func(t *testing.T) {
		explicit := params
		explicit.RuntimeProfileUUID = "33333333-3333-4333-8333-333333333333"
		assertMapperBuilderContract(t, mapperBuilderContract{
			statement:         consoleWorkspaceMapperInsertStatement,
			bound:             buildConsoleWorkspaceMapperInsert(yourbatis.DialectPostgres, explicit),
			wantID:            "ConsoleWorkspaceMapper.Insert",
			wantKind:          yourbatis.StatementInsert,
			wantArgumentNames: []string{"params.OrgUUID", "params.UUID", "params.ExternalID", "params.Name", "params.ExternalID", "params.DisplayColor", "params.DataResidency", "params.RuntimeProfileUUID"},
			wantSQLFragments:  []string{"runtime_profile_uuid", "$8", "RETURNING"},
		})
	})

	t.Run("update never mutates runtime_profile_uuid", func(t *testing.T) {
		bound := buildConsoleWorkspaceMapperUpdateByIdentifier(yourbatis.DialectPostgres, updateConsoleWorkspaceParams{
			OrgUUID:      orgUUID,
			ExternalID:   "workspace_external",
			Name:         "Renamed",
			DisplayColor: "#111111",
		})
		setPart := bound.SQL[:strings.Index(bound.SQL, "WHERE")]
		if strings.Contains(setPart, "runtime_profile_uuid") {
			t.Fatalf("UpdateByIdentifier SET clause must not touch runtime_profile_uuid: %q", setPart)
		}
	})

	t.Run("active only", func(t *testing.T) {
		bound := buildConsoleWorkspaceMapperList(yourbatis.DialectPostgres, orgUUID, false)
		assertMapperBuilderContract(t, mapperBuilderContract{
			statement:         consoleWorkspaceMapperListStatement,
			bound:             bound,
			wantID:            "ConsoleWorkspaceMapper.List",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"orgUUID"},
			wantSQLFragments:  []string{"LEFT JOIN runtime_profiles rp", "w.organization_uuid = $1", "w.archived_at IS NULL", "ORDER BY w.name ASC"},
		})
	})

	t.Run("including archived", func(t *testing.T) {
		bound := buildConsoleWorkspaceMapperList(yourbatis.DialectPostgres, orgUUID, true)
		if strings.Contains(bound.SQL, "archived_at IS NULL") {
			t.Fatalf("generated SQL unexpectedly filters archived rows: %q", bound.SQL)
		}
	})
}

func TestConsoleWorkspaceRowMapsJSONAndRuntimeFields(t *testing.T) {
	row := consoleWorkspaceRow{
		UUID:                     "22222222-2222-4222-8222-222222222222",
		ExternalID:               "workspace_external",
		OrgUUID:                  "11111111-1111-4111-8111-111111111111",
		DataResidency:            []byte(`{"workspace_geo":"us","allowed_inference_geos":"unrestricted","default_inference_geo":"global"}`),
		Tags:                     []byte(`{"team":"platform"}`),
		RuntimeProfileUUID:       DefaultOMANativeRuntimeProfileUUID,
		RuntimeProfileExternalID: "rprof_oma_native_default",
		RuntimeProfileName:       "OMA Native",
		RuntimeProviderKind:      "oma_native",
	}

	workspace, err := row.workspace()
	if err != nil {
		t.Fatalf("workspace(): %v", err)
	}
	if workspace.DataResidency == nil || *workspace.DataResidency != "us" {
		t.Fatalf("data residency = %#v, want us", workspace.DataResidency)
	}
	if workspace.UUID != row.UUID || workspace.ExternalID != row.ExternalID {
		t.Fatalf("workspace identifiers = (%q, %q)", workspace.UUID, workspace.ExternalID)
	}
	if workspace.Tags["team"] != "platform" {
		t.Fatalf("tags = %#v, want platform team", workspace.Tags)
	}
	if workspace.RuntimeProfileUUID != DefaultOMANativeRuntimeProfileUUID ||
		workspace.RuntimeProfileExternalID != "rprof_oma_native_default" ||
		workspace.RuntimeProfileName != "OMA Native" ||
		workspace.RuntimeProviderKind != "oma_native" {
		t.Fatalf("runtime profile metadata = %+v", workspace)
	}
}
