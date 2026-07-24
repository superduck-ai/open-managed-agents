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
				"workspace_id":        int64(42),
				"session_external_id": "session_test",
				"resource_type":       SessionResourceTypeFile,
			},
			wantArgCount: 3,
			wantClauses: []string{
				"workspace_id = $1",
				"session_external_id = $2",
				"resource_type = $3",
			},
		},
		{
			name:  "managed mount conflict",
			query: findSessionFileMountConflictSQL,
			arguments: map[string]any{
				"workspace_uuid":  "00000000-0000-0000-0000-000000000001",
				"filesystem_uuid": "00000000-0000-0000-0000-000000000002",
				"managed_by":      sessionFileResourceManagedBy,
				"entry_path":      "/uploads/workspace/data.csv",
			},
			wantArgCount: 4,
			wantClauses: []string{
				"CAST($1 AS text)",
				"entry.workspace_uuid = $2",
				"entry.filesystem_uuid = $3",
				"entry.managed_by = $4",
				"entry.path = candidate.path",
				"left(entry.path, length(candidate.path) + 1)",
				"left(candidate.path, length(entry.path) + 1)",
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
