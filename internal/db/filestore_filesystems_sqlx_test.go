package db

import (
	"strings"
	"testing"
	"time"
)

func TestProvisionFilestoreFilesystemQueriesUseSQLXNamedParameters(t *testing.T) {
	codeSessionUUID := "00000000-0000-0000-0000-000000000004"
	apiKeyUUID := "00000000-0000-0000-0000-000000000005"
	arguments := provisionFilestoreFilesystemArguments(ProvisionFilestoreFilesystemInput{
		UUID:                "00000000-0000-0000-0000-000000000001",
		ExternalID:          "claude_chat_sqlx",
		OrganizationUUID:    "00000000-0000-0000-0000-000000000002",
		WorkspaceUUID:       "00000000-0000-0000-0000-000000000003",
		SessionUUID:         "00000000-0000-0000-0000-000000000006",
		CodeSessionUUID:     &codeSessionUUID,
		CreatedByAPIKeyUUID: &apiKeyUUID,
		Now:                 time.Date(2026, time.July, 23, 17, 0, 0, 0, time.UTC),
	})
	arguments["workspace_id"] = int64(42)
	arguments["filesystem_id"] = int64(99)

	tests := []struct {
		name         string
		query        string
		wantArgCount int
	}{
		{"provision advisory lock", provisionFilestoreAdvisoryLockQuery, 2},
		{"session binding", validateFilestoreSessionBindingQuery, 7},
		{"workspace lock", provisionFilestoreWorkspaceLockQuery, 1},
		{"lookup by external ID", filestoreFilesystemSelectSQL() + provisionFilestoreByExternalIDQuery, 4},
		{"lookup by Session", filestoreFilesystemSelectSQL() + provisionFilestoreBySessionQuery, 2},
		{"insert filesystem", insertProvisionedFilestoreQuery, 9},
		{"namespace lock", provisionFilestoreNamespaceLockQuery, 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			boundQuery, values, err := bindNamed(postgresRebinder{}, test.query, arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			for argumentName := range arguments {
				if strings.Contains(boundQuery, ":"+argumentName) {
					t.Fatalf("query retains named parameter %q: %q", argumentName, boundQuery)
				}
			}
			if strings.Contains(boundQuery, "::") {
				t.Fatalf("query uses PostgreSQL colon cast syntax: %q", boundQuery)
			}
			if len(values) != test.wantArgCount {
				t.Fatalf("bound argument count = %d, want %d", len(values), test.wantArgCount)
			}
		})
	}
}
