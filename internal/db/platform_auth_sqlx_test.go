package db

import (
	"strings"
	"testing"
)

func TestPlatformAuthQueriesUseSQLXNamedParameters(t *testing.T) {
	t.Run("rejects a missing named argument", func(t *testing.T) {
		if _, _, err := bindNamed(postgresRebinder{}, resolvePlatformSessionIdentityQuery, map[string]any{
			"org_uuid": "org_test",
		}); err == nil {
			t.Fatal("bindNamed() error = nil, want missing user_uuid error")
		}
	})

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "find user context",
			query:        findPlatformAuthUserContextQuery,
			arguments:    map[string]any{"email": "owner@example.com"},
			wantArgCount: 1,
		},
		{
			name:  "resolve session identity",
			query: resolvePlatformSessionIdentityQuery,
			arguments: map[string]any{
				"org_uuid":  "org_test",
				"user_uuid": "user_test",
			},
			wantArgCount: 4,
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
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}

func TestPlatformSessionIdentityRowMapping(t *testing.T) {
	row := platformSessionIdentityRow{
		OrganizationUUID:    "org-uuid",
		WorkspaceUUID:       "workspace-uuid",
		WorkspaceExternalID: "workspace_test",
		UserUUID:            "user-uuid",
		UserExternalID:      "user_test",
		APIKeyUUID:          "api-key-uuid",
		APIKeyExternalID:    "api_key_test",
	}

	session := row.session()
	if session.OrganizationUUID != row.OrganizationUUID ||
		session.WorkspaceExternalID != row.WorkspaceExternalID ||
		session.UserUUID != row.UserUUID ||
		session.APIKeyUUID != row.APIKeyUUID ||
		session.APIKeyExternalID != row.APIKeyExternalID {
		t.Fatalf("session = %#v, want values from row %#v", session, row)
	}
}
