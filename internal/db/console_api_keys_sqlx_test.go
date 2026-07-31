package db

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestConsoleAPIKeyQueriesUseSQLXNamedParameters(t *testing.T) {
	orgUUID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	workspaceUUID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	apiKeyQuery, apiKeyArguments := listConsoleAPIKeysQuery(orgUUID, uuid.NullUUID{UUID: workspaceUUID, Valid: true})
	workspaceQuery, workspaceArguments := listConsoleWorkspacesQuery(orgUUID, false)

	t.Run("rejects a missing named argument", func(t *testing.T) {
		if _, _, err := bindNamed(postgresRebinder{}, apiKeyQuery, map[string]any{
			"org_uuid": orgUUID,
		}); err == nil {
			t.Fatal("bindNamed() error = nil, want missing workspace_uuid error")
		}
	})

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "list api keys by workspace",
			query:        apiKeyQuery,
			arguments:    apiKeyArguments,
			wantArgCount: 2,
		},
		{
			name:         "list workspaces",
			query:        workspaceQuery,
			arguments:    workspaceArguments,
			wantArgCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains named parameter syntax: %q", query)
			}
			if strings.Contains(test.query, "::") {
				t.Fatalf("query uses PostgreSQL cast shorthand: %q", test.query)
			}
			if strings.Contains(test.query, " AS uuid)") || strings.Contains(test.query, " AS text)") {
				t.Fatalf("query contains UUID cast ceremony: %q", test.query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}

func TestConsoleWorkspaceRowMapsJSONFields(t *testing.T) {
	row := consoleWorkspaceRow{
		UUID:          uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		ExternalID:    "workspace_external",
		OrgUUID:       uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		DataResidency: []byte(`{"workspace_geo":"us","allowed_inference_geos":"unrestricted","default_inference_geo":"global"}`),
		Tags:          []byte(`{"team":"platform"}`),
	}

	workspace, err := row.workspace()
	if err != nil {
		t.Fatalf("workspace(): %v", err)
	}
	if workspace.DataResidency == nil || *workspace.DataResidency != "us" {
		t.Fatalf("data residency = %#v, want us", workspace.DataResidency)
	}
	if workspace.UUID != "22222222-2222-4222-8222-222222222222" || workspace.ExternalID != "workspace_external" {
		t.Fatalf("workspace identifiers = (%q, %q), want database UUID and external ID", workspace.UUID, workspace.ExternalID)
	}
	if workspace.Tags["team"] != "platform" {
		t.Fatalf("tags = %#v, want platform team", workspace.Tags)
	}
}
