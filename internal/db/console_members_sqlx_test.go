package db

import (
	"strings"
	"testing"

	"github.com/google/uuid"
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
				"org_uuid": uuid.MustParse("11111111-1111-4111-8111-111111111111"),
				"limit":    100,
			},
			wantArgCount: 2,
		},
		{
			name:  "update role",
			query: updateOrgUserRoleQuery,
			arguments: map[string]any{
				"org_uuid":  uuid.MustParse("11111111-1111-4111-8111-111111111111"),
				"user_id":   "user_test",
				"user_uuid": tryParseDBUUIDIdentifier("user_test"),
				"role":      "developer",
			},
			wantArgCount: 5,
		},
		{
			name:  "remove user",
			query: removeOrgUserQuery,
			arguments: map[string]any{
				"org_uuid":  uuid.MustParse("11111111-1111-4111-8111-111111111111"),
				"user_id":   "user_test",
				"user_uuid": tryParseDBUUIDIdentifier("user_test"),
			},
			wantArgCount: 4,
		},
		{
			name:  "remove workspace memberships",
			query: removeOrgUserWorkspaceMembershipsQuery,
			arguments: map[string]any{
				"org_uuid":  uuid.MustParse("11111111-1111-4111-8111-111111111111"),
				"user_id":   "user_test",
				"user_uuid": tryParseDBUUIDIdentifier("user_test"),
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
