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
			name:  "bind Session metadata",
			query: bindManagedAgentSessionMetadataQuery,
			arguments: map[string]any{
				"organization_id":     int64(1),
				"workspace_id":        int64(2),
				"session_external_id": "sesn_test",
				"metadata_patch":      `{"runtime":"claude_code_local"}`,
			},
			wantArgCount: 4,
		},
		{
			name:  "bind Environment Work metadata",
			query: bindManagedAgentWorkMetadataQuery,
			arguments: map[string]any{
				"organization_id":         int64(1),
				"workspace_id":            int64(2),
				"environment_id":          int64(3),
				"environment_external_id": "env_test",
				"work_external_id":        "envwork_test",
				"metadata_patch":          `{"runtime":"claude_code_local"}`,
			},
			wantArgCount: 6,
		},
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
