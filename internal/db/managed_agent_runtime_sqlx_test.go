package db

import (
	"strings"
	"testing"
)

func TestManagedAgentRuntimeQueriesUseSQLXNamedParameters(t *testing.T) {
	arguments := map[string]any{
		"code_session_external_id": "cse_test",
		"code_session_id":          int64(3),
		"created_at":               true,
		"delivery_status":          true,
		"environment_external_id":  "env_test",
		"environment_id":           int64(4),
		"event_subtype":            true,
		"event_type":               true,
		"external_id":              "external_test",
		"idempotency_key":          true,
		"metadata":                 true,
		"metadata_patch":           true,
		"model":                    true,
		"oauth_access_token_hash":  true,
		"organization_id":          int64(1),
		"payload":                  true,
		"payload_hash":             true,
		"payload_uuid":             true,
		"permission_mode":          true,
		"request_id":               true,
		"runtime_patch":            true,
		"sequence_num":             int64(1),
		"session_external_id":      "session_test",
		"session_id":               int64(3),
		"source":                   true,
		"status":                   true,
		"work_dir":                 true,
		"work_external_id":         "envwork_test",
		"workspace_id":             int64(2),
	}
	tests := []struct {
		name  string
		query string
	}{
		{"lock Environment Work", lockManagedAgentEnvironmentWorkQuery},
		{"list locked Session events", listManagedAgentSessionEventsQuery},
		{"create Code Session", createCodeSessionQuery},
		{"append inbound event", insertCodeSessionInboundEventQuery},
		{"advance inbound sequence", updateCodeSessionInboundSequenceQuery},
		{"patch Session metadata", patchSessionMetadataQuery},
		{"patch Work metadata", patchManagedAgentWorkMetadataQuery},
		{"credential context", codeSessionCredentialContextForIssueQuery},
		{"terminate Code Session", terminateManagedAgentCodeSessionQuery},
		{"clear Session metadata", clearTerminatedManagedAgentSessionMetadataQuery},
		{"clear Work metadata", clearTerminatedManagedAgentWorkMetadataQuery},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, _, err := bindNamed(postgresRebinder{}, test.query, arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains named parameter or double-colon cast syntax: %q", query)
			}
		})
	}
}
