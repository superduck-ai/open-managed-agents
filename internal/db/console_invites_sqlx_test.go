package db

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestConsoleInviteQueriesUseSQLXNamedParameters(t *testing.T) {
	now := time.Date(2026, time.July, 27, 12, 30, 0, 0, time.UTC)
	listQuery := `
		select ` + consoleInviteColumns + `
		from organization_invites i
		where i.organization_uuid = :org_uuid
			and i.deleted_at is null
		order by i.invited_at desc, i.uuid desc
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
				"org_uuid": uuid.MustParse("11111111-1111-4111-8111-111111111111"),
				"limit":    100,
			},
			wantArgCount: 2,
		},
		{
			name:  "create",
			query: createConsoleInviteQuery,
			arguments: map[string]any{
				"org_uuid":    uuid.MustParse("11111111-1111-4111-8111-111111111111"),
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
				"org_uuid":   uuid.MustParse("11111111-1111-4111-8111-111111111111"),
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
				"org_uuid":  uuid.MustParse("11111111-1111-4111-8111-111111111111"),
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
			if strings.Contains(query, "CAST($1 AS uuid)") {
				t.Fatalf("bound query contains UUID parameter cast: %s", query)
			}
		})
	}
}
