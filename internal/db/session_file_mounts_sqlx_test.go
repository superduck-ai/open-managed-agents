package db

import (
	"strings"
	"testing"
)

func TestSessionFileResourceQueriesBindNamedArguments(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
		wantClauses  []string
	}{
		{
			name:  "active file count",
			query: countSessionFileResourcesSQL,
			arguments: map[string]any{
				"workspace_uuid":      dbUUID("00000000-0000-4000-8000-000000000042"),
				"session_external_id": "session_test",
				"resource_type":       SessionResourceTypeFile,
			},
			wantArgCount: 3,
			wantClauses: []string{
				"workspace_uuid = $1",
				"session_external_id = $2",
				"resource_type = $3",
				"payload is not null",
			},
		},
		{
			name:  "input resource mount conflict",
			query: findSessionFileMountConflictSQL,
			arguments: map[string]any{
				"workspace_uuid": dbUUID("00000000-0000-4000-8000-000000000001"),
				"session_uuid":   dbUUID("00000000-0000-4000-8000-000000000002"),
				"entry_path":     "/uploads/workspace/data.csv",
			},
			wantArgCount: 3,
			wantClauses: []string{
				"CAST($1 AS text)",
				"resource.workspace_uuid = $2",
				"resource.session_uuid = $3",
				"resource.path = candidate.path",
				"left(resource.path, length(candidate.path) + 1)",
				"left(candidate.path, length(resource.path) + 1)",
			},
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
			for _, clause := range test.wantClauses {
				if !strings.Contains(query, clause) {
					t.Fatalf("bound query does not contain %q: %q", clause, query)
				}
			}
		})
	}
}
