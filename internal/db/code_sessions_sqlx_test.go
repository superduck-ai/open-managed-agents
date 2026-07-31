package db

import (
	"strings"
	"testing"
)

func TestCodeSessionQueriesBindNamedArguments(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:         "oauth credential context",
			query:        codeSessionByOAuthAccessTokenHashQuery,
			arguments:    map[string]any{"token_hash": "oauth-token-hash"},
			wantArgCount: 1,
		},
		{
			name:  "credential context for issue",
			query: codeSessionCredentialContextForIssueQuery,
			arguments: map[string]any{
				"code_session_external_id": "codeses_test",
				"organization_uuid":        "00000000-0000-0000-0000-000000000001",
				"workspace_uuid":           "00000000-0000-0000-0000-000000000002",
			},
			wantArgCount: 3,
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
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}

func TestCodeSessionSQLXColumnListsAvoidPostgreSQLShorthandCasts(t *testing.T) {
	columnLists := []string{
		codeSessionColumns(),
		codeSessionInboundEventColumns(),
		codeSessionInboundEventColumnsWithAlias("e"),
		codeSessionOutboundEventColumns(),
		codeSessionInternalEventColumns(),
		codeSessionInternalEventColumnsWithAlias("e"),
	}
	for _, columns := range columnLists {
		if strings.Contains(columns, "::") {
			t.Fatalf("column list contains shorthand cast that conflicts with sqlx named parsing: %q", columns)
		}
	}
}
