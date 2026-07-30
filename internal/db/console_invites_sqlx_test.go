package db

import (
	"strings"
	"testing"
	"time"
)

func TestConsoleInviteQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 12, 30, 0, 0, time.UTC)
	listQuery := `
		select ` + consoleInviteColumns + `
		from organization_invites i
		join organizations o on o.id = i.organization_id
		where CAST(o.uuid AS text) = :org_uuid
			and i.deleted_at is null
		order by i.invited_at desc, i.id desc
		limit :limit
	`
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "list",
			query: listQuery,
			arguments: map[string]any{
				"org_uuid": "org_test",
				"limit":    100,
			},
			wantArgCount: 2,
		},
		{
			name:  "create",
			query: createConsoleInviteQuery,
			arguments: map[string]any{
				"org_uuid":    "org_test",
				"external_id": "invite_test",
				"email":       "invite@example.test",
				"role":        "developer",
				"invited_at":  now,
				"expires_at":  now.Add(21 * 24 * time.Hour),
			},
			wantArgCount: 6,
		},
		{
			name:  "resend",
			query: resendConsoleInviteQuery,
			arguments: map[string]any{
				"org_uuid":   "org_test",
				"invite_id":  "invite_test",
				"invited_at": now,
				"expires_at": now.Add(21 * 24 * time.Hour),
			},
			wantArgCount: 4,
		},
		{
			name:  "delete",
			query: deleteConsoleInviteQuery,
			arguments: map[string]any{
				"org_uuid":  "org_test",
				"invite_id": "invite_test",
			},
			wantArgCount: 2,
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
