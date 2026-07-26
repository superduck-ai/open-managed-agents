package db

import (
	"strings"
	"testing"
)

func TestManagedAgentRuntimeQueriesUseSQLXNamedParameters(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "terminate Code Session",
			query: terminateManagedAgentCodeSessionQuery,
			arguments: map[string]any{
				"organization_id":          int64(1),
				"workspace_id":             int64(2),
				"code_session_external_id": "cse_test",
			},
			wantArgCount: 3,
		},
		{
			name:  "clear terminated Session metadata",
			query: clearTerminatedManagedAgentSessionMetadataQuery,
			arguments: map[string]any{
				"organization_id":          int64(1),
				"workspace_id":             int64(2),
				"session_external_id":      "session_test",
				"code_session_external_id": "cse_test",
			},
			wantArgCount: 4,
		},
		{
			name:  "clear terminated Work metadata",
			query: clearTerminatedManagedAgentWorkMetadataQuery,
			arguments: map[string]any{
				"organization_id":          int64(1),
				"workspace_id":             int64(2),
				"code_session_external_id": "cse_test",
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
