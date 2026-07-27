package db

import (
	"strings"
	"testing"
)

func TestConsoleMemberQueriesUseSQLXNamedParameters(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "list",
			query: listOrgUsersQuery,
			arguments: map[string]any{
				"org_uuid": "org_test",
				"limit":    100,
			},
			wantArgCount: 3,
		},
		{
			name:  "update role",
			query: updateOrgUserRoleQuery,
			arguments: map[string]any{
				"org_uuid": "org_test",
				"user_id":  "user_test",
				"role":     "developer",
			},
			wantArgCount: 6,
		},
		{
			name:  "remove user",
			query: removeOrgUserQuery,
			arguments: map[string]any{
				"org_uuid": "org_test",
				"user_id":  "user_test",
			},
			wantArgCount: 5,
		},
		{
			name:  "remove workspace memberships",
			query: removeOrgUserWorkspaceMembershipsQuery,
			arguments: map[string]any{
				"org_uuid": "org_test",
				"user_id":  "user_test",
			},
			wantArgCount: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, tt.query, tt.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if len(arguments) != tt.wantArgCount {
				t.Fatalf("bindNamed() arguments = %#v, want %d arguments", arguments, tt.wantArgCount)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("bound query still contains a named parameter: %s", query)
			}
		})
	}
}
