package db

import (
	"strings"
	"testing"
	"time"
)

var testSQLXTimestamp = time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC)

// TestManagedAgentRuntimeQueriesRejectDoubleColonCasts 守住 sqlx 命名参数规则：
// `value::type` 的冒号会与命名参数解析冲突，必须写成 CAST(value AS type)。
func TestManagedAgentRuntimeQueriesRejectDoubleColonCasts(t *testing.T) {
	queries := map[string]string{
		"lock Environment Work":                 lockManagedAgentEnvironmentWorkQuery,
		"list locked Session events":            listManagedAgentSessionEventsQuery,
		"create Code Session":                   createCodeSessionQuery,
		"append Code Session inbound event":     insertCodeSessionInboundEventQuery,
		"advance Code Session inbound sequence": updateCodeSessionInboundSequenceQuery,
		"patch Session metadata":                patchSessionMetadataQuery,
		"patch Environment Work metadata":       patchManagedAgentWorkMetadataQuery,
		"issue Code Session credential":         codeSessionCredentialContextForIssueQuery,
		"terminate Code Session":                terminateManagedAgentCodeSessionQuery,
		"clear terminated Session metadata":     clearTerminatedManagedAgentSessionMetadataQuery,
		"clear terminated Work metadata":        clearTerminatedManagedAgentWorkMetadataQuery,
	}
	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(query, "::") {
				t.Fatalf("query uses a :: cast that conflicts with named parameters: %q", query)
			}
		})
	}
}

func TestManagedAgentRuntimeQueriesUseSQLXNamedParameters(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "lock Environment Work",
			query: lockManagedAgentEnvironmentWorkQuery,
			arguments: map[string]any{
				"workspace_id":            int64(2),
				"environment_external_id": "env_test",
				"work_external_id":        "envwork_test",
			},
			wantArgCount: 3,
		},
		{
			name:  "list locked Session events",
			query: listManagedAgentSessionEventsQuery,
			arguments: map[string]any{
				"workspace_id":        int64(2),
				"session_external_id": "session_test",
			},
			wantArgCount: 2,
		},
		{
			name:  "create Code Session",
			query: createCodeSessionQuery,
			arguments: map[string]any{
				"code_session_external_id": "cse_test",
				"organization_id":          int64(1),
				"workspace_id":             int64(2),
				"session_id":               int64(3),
				"session_external_id":      "session_test",
				"environment_id":           int64(4),
				"environment_external_id":  "env_test",
				"work_dir":                 "/workspace",
				"permission_mode":          "bypassPermissions",
				"model":                    "claude-test",
				"status":                   "active",
				"metadata":                 []byte(`{"source":"managed_agents_local"}`),
				"oauth_access_token_hash":  "hash",
				"created_at":               testSQLXTimestamp,
			},
			// created_at 同时绑定 created_at 与 updated_at，因此比命名参数多一位。
			wantArgCount: 15,
		},
		{
			name:  "append Code Session inbound event",
			query: insertCodeSessionInboundEventQuery,
			arguments: map[string]any{
				"event_external_id":        "csev_test",
				"organization_id":          int64(1),
				"workspace_id":             int64(2),
				"code_session_id":          int64(3),
				"code_session_external_id": "cse_test",
				"sequence_num":             int64(1),
				"event_type":               "control_request",
				"event_subtype":            "initialize",
				"payload_uuid":             nil,
				"request_id":               nil,
				"payload":                  []byte(`{"type":"control_request"}`),
				"payload_hash":             "hash",
				"idempotency_key":          "key",
				"delivery_status":          "queued",
				"source":                   "internal",
				"created_at":               testSQLXTimestamp,
			},
			wantArgCount: 17,
		},
		{
			name:  "advance Code Session inbound sequence",
			query: updateCodeSessionInboundSequenceQuery,
			arguments: map[string]any{
				"sequence_num":    int64(4),
				"code_session_id": int64(3),
			},
			wantArgCount: 2,
		},
		{
			name:  "patch Session metadata",
			query: patchSessionMetadataQuery,
			arguments: map[string]any{
				"workspace_id":        int64(2),
				"session_external_id": "session_test",
				"metadata_patch":      []byte(`{"runtime":"claude_code_local"}`),
			},
			wantArgCount: 3,
		},
		{
			name:  "patch Environment Work metadata",
			query: patchManagedAgentWorkMetadataQuery,
			arguments: map[string]any{
				"workspace_id":            int64(2),
				"environment_external_id": "env_test",
				"work_external_id":        "envwork_test",
				"runtime_patch":           []byte(`{"runtime":"claude_code_local"}`),
			},
			wantArgCount: 4,
		},
		{
			name:  "issue Code Session credential context",
			query: codeSessionCredentialContextForIssueQuery,
			arguments: map[string]any{
				"code_session_external_id": "cse_test",
				"organization_id":          int64(1),
				"workspace_id":             int64(2),
			},
			wantArgCount: 3,
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
