package db

import (
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionFileResourceMapperBuildsPostgresArguments(t *testing.T) {
	tests := []struct {
		name         string
		bound        yourbatis.BoundSQL
		wantArgCount int
		wantClauses  []string
	}{
		{
			name: "active file count",
			bound: buildSessionResourceMapperCountSessionFileResources(
				yourbatis.DialectPostgres,
				"00000000-0000-4000-8000-000000000042",
				"session_test",
				SessionResourceTypeFile,
			),
			wantArgCount: 3,
			wantClauses: []string{
				"workspace_uuid = $1",
				"session_external_id = $2",
				"resource_type = $3",
				"payload IS NOT NULL",
			},
		},
		{
			name: "input resource mount conflict",
			bound: buildSessionResourceMapperFindMountConflict(yourbatis.DialectPostgres, sessionResourcePathParams{
				WorkspaceUUID: "00000000-0000-4000-8000-000000000001",
				SessionUUID:   "00000000-0000-4000-8000-000000000002",
				EntryPath:     "/uploads/workspace/data.csv",
			}),
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
			if len(test.bound.Args) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(test.bound.Args), test.wantArgCount)
			}
			for _, clause := range test.wantClauses {
				if !strings.Contains(test.bound.SQL, clause) {
					t.Fatalf("bound query does not contain %q: %q", clause, test.bound.SQL)
				}
			}
		})
	}
}
