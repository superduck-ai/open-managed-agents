package db

import (
	"strings"
	"testing"
)

func TestDatabaseQueriesUseSQLXNamedParameters(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "maintenance role",
			query:        maintenanceRoleExistsQuery,
			arguments:    map[string]any{"role": "app"},
			wantArgCount: 1,
		},
		{
			name:         "maintenance database",
			query:        maintenanceDatabaseExistsQuery,
			arguments:    map[string]any{"database_name": "app"},
			wantArgCount: 1,
		},
		{
			name:         "id column type",
			query:        idColumnDataTypeQuery,
			arguments:    map[string]any{"table_name": "organizations"},
			wantArgCount: 1,
		},
		{
			name:         "legacy table",
			query:        legacyTableExistsQuery,
			arguments:    map[string]any{"table_name": "organizations_legacy_text_ids"},
			wantArgCount: 1,
		},
		{
			name:  "seed organization",
			query: seedOrganizationQuery,
			arguments: map[string]any{
				"external_id": "org_default",
				"name":        "default",
			},
			wantArgCount: 2,
		},
		{
			name:  "seed workspace",
			query: seedWorkspaceQuery,
			arguments: map[string]any{
				"external_id":     "workspace_default",
				"organization_id": int64(1),
				"name":            "default",
			},
			wantArgCount: 3,
		},
		{
			name:  "seed user",
			query: seedUserQuery,
			arguments: map[string]any{
				"external_id":     "user_default",
				"organization_id": int64(1),
				"email":           "admin@example.local",
				"name":            "Local Admin",
			},
			wantArgCount: 4,
		},
		{
			name:  "seed workspace member",
			query: seedWorkspaceMemberQuery,
			arguments: map[string]any{
				"external_id":           "wmem_default",
				"organization_id":       int64(1),
				"workspace_id":          int64(2),
				"workspace_external_id": "workspace_default",
				"user_id":               int64(3),
				"user_external_id":      "user_default",
			},
			wantArgCount: 6,
		},
		{
			name:  "seed API key",
			query: seedAPIKeyQuery,
			arguments: map[string]any{
				"external_id":        "sk_default",
				"workspace_id":       int64(2),
				"key_hash":           "hash",
				"created_by_user_id": int64(3),
				"name":               "sk_default",
				"partial_key_hint":   "sk-ant-l...ault",
			},
			wantArgCount: 6,
		},
		{
			name:         "get API key",
			query:        getAPIKeyQuery,
			arguments:    map[string]any{"key_hash": "hash"},
			wantArgCount: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("bound query retains named parameters: %q", query)
			}
			if strings.Contains(query, "::") {
				t.Fatalf("bound query contains PostgreSQL shorthand cast: %q", query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("bound argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}

func TestAPIKeyRowMapsDatabaseColumns(t *testing.T) {
	row := apiKeyRow{
		ID:                     1,
		ExternalID:             "sk_default",
		OrganizationID:         2,
		OrganizationExternalID: "org_default",
		WorkspaceID:            3,
		WorkspaceUUID:          "11111111-1111-4111-8111-111111111111",
		WorkspaceExternalID:    "workspace_default",
	}

	key := row.apiKey()
	if key.ExternalID != row.ExternalID ||
		key.OrganizationExternalID != row.OrganizationExternalID ||
		key.WorkspaceUUID != row.WorkspaceUUID {
		t.Fatalf("apiKeyRow.apiKey() = %#v, want values from %#v", key, row)
	}
}
