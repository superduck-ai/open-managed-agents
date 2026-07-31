package db

import (
	"strings"
	"testing"
	"time"
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
				"workspace_uuid":      "00000000-0000-0000-0000-000000000001",
				"session_external_id": "session_test",
				"resource_type":       SessionResourceTypeFile,
			},
			wantArgCount: 3,
			wantClauses: []string{
				"workspace_uuid = $1",
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

func TestSessionFileProjectionQueriesBindNamedArguments(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		arguments   map[string]any
		wantClauses []string
	}{
		{
			name:  "upsert projection",
			query: upsertSessionFileProjectionSQL,
			arguments: map[string]any{
				"file_uuid":               "00000000-0000-0000-0000-000000000001",
				"workspace_uuid":          "00000000-0000-0000-0000-000000000002",
				"filename":                "input.txt",
				"mime_type":               "text/plain",
				"size_bytes":              int64(5),
				"sha256":                  strings.Repeat("a", 64),
				"s3_bucket":               "bucket",
				"s3_key":                  "key",
				"downloadable":            true,
				"scope_type":              sessionFileProjectionScope,
				"scope_id":                "session_test",
				"created_by_api_key_uuid": "00000000-0000-0000-0000-000000000003",
				"created_at":              time.Unix(1, 0).UTC(),
			},
			wantClauses: []string{
				"CAST($1 AS uuid)",
				"on conflict (uuid) do update",
				"deleted_at = null",
			},
		},
		{
			name:  "soft delete projection",
			query: softDeleteSessionFileProjectionSQL,
			arguments: map[string]any{
				"workspace_uuid": "00000000-0000-0000-0000-000000000002",
				"file_uuid":      "00000000-0000-0000-0000-000000000001",
				"scope_type":     sessionFileProjectionScope,
				"scope_id":       "session_test",
			},
			wantClauses: []string{
				"workspace_uuid = $1",
				"uuid = CAST($2 AS uuid)",
				"scope_type = $3",
				"scope_id = $4",
			},
		},
		{
			name:  "soft delete projections by scope",
			query: softDeleteSessionFileProjectionsByScopeSQL,
			arguments: map[string]any{
				"workspace_uuid": "00000000-0000-0000-0000-000000000002",
				"scope_type":     sessionFileProjectionScope,
				"scope_id":       "session_test",
			},
			wantClauses: []string{
				"workspace_uuid = $1",
				"scope_type = $2",
				"scope_id = $3",
			},
		},
		{
			name:  "soft delete projection by entry",
			query: softDeleteSessionFileProjectionByEntrySQL,
			arguments: map[string]any{
				"workspace_uuid": "00000000-0000-0000-0000-000000000002",
				"scope_type":     sessionFileProjectionScope,
				"file_uuid":      "00000000-0000-0000-0000-000000000001",
			},
			wantClauses: []string{
				"projection.workspace_uuid = $1",
				"projection.scope_type = $2",
				"projection.uuid = CAST($3 AS uuid)",
			},
		},
		{
			name:  "soft delete projection subtree",
			query: softDeleteSessionFileProjectionSubtreeSQL,
			arguments: map[string]any{
				"workspace_uuid":  "00000000-0000-0000-0000-000000000001",
				"scope_type":      sessionFileProjectionScope,
				"filesystem_uuid": "00000000-0000-0000-0000-000000000002",
				"root_path":       "/outputs/reports",
			},
			wantClauses: []string{
				"projection.workspace_uuid = $1",
				"projection.scope_type = $2",
				"entry.workspace_uuid = $3",
				"entry.filesystem_uuid = $4",
				"entry.path = $5",
				"left(entry.path, char_length($6) + 1)",
				"= $7 || '/'",
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
			if len(arguments) == 0 {
				t.Fatal("bound query has no arguments")
			}
			for _, clause := range test.wantClauses {
				if !strings.Contains(query, clause) {
					t.Fatalf("bound query does not contain %q: %q", clause, query)
				}
			}
		})
	}
}
