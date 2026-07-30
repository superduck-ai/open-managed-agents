package db

import (
	"strings"
	"testing"
	"time"
)

func TestMigratedSessionQueriesBindNamedArguments(t *testing.T) {
	now := time.Date(2026, time.July, 27, 16, 0, 0, 0, time.UTC)
	threadArguments := createSessionThreadArguments(SessionThread{
		UUID:              "11111111-1111-4111-8111-111111111111",
		ExternalID:        "sesthr_test",
		OrganizationID:    1,
		WorkspaceID:       2,
		SessionID:         3,
		SessionExternalID: "sesn_test",
		AgentSnapshot:     []byte(`{"model":"test"}`),
		Status:            "idle",
		Usage:             []byte(`{}`),
		Stats:             []byte(`{}`),
		CreatedAt:         now,
	})

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{
			name:  "update session",
			query: updateSessionQuery,
			arguments: map[string]any{
				"workspace_id":        int64(2),
				"session_external_id": "sesn_test",
				"agent_snapshot":      []byte(`{}`),
				"title":               "title",
				"metadata":            []byte(`{}`),
				"updated_at":          now,
			},
			wantArgCount: 6,
		},
		{
			name:  "patch metadata",
			query: patchSessionMetadataQuery,
			arguments: map[string]any{
				"workspace_id":        int64(2),
				"session_external_id": "sesn_test",
				"metadata_patch":      []byte(`{}`),
			},
			wantArgCount: 3,
		},
		{
			name:  "set evaluations",
			query: setSessionOutcomeEvaluationsQuery,
			arguments: map[string]any{
				"workspace_id":        int64(2),
				"session_external_id": "sesn_test",
				"outcome_evaluations": []byte(`[]`),
			},
			wantArgCount: 3,
		},
		{
			name:  "set session status",
			query: setSessionStatusQuery,
			arguments: map[string]any{
				"workspace_id":        int64(2),
				"session_external_id": "sesn_test",
				"status":              "idle",
			},
			wantArgCount: 3,
		},
		{
			name:  "set thread status",
			query: setSessionThreadStatusQuery,
			arguments: map[string]any{
				"workspace_id":        int64(2),
				"session_external_id": "sesn_test",
				"thread_external_id":  "sesthr_test",
				"status":              "idle",
			},
			wantArgCount: 4,
		},
		{
			name:         "create thread if absent",
			query:        createSessionThreadIfAbsentQuery,
			arguments:    threadArguments,
			wantArgCount: 14,
		},
		{
			name:         "archive session",
			query:        archiveSessionQuery,
			arguments:    sessionLookupArguments(2, "sesn_test"),
			wantArgCount: 2,
		},
		{
			name:         "delete session",
			query:        deleteSessionQuery,
			arguments:    sessionLookupArguments(2, "sesn_test"),
			wantArgCount: 2,
		},
		{
			name:         "delete session threads",
			query:        deleteSessionThreadsQuery,
			arguments:    sessionLookupArguments(2, "sesn_test"),
			wantArgCount: 2,
		},
		{
			name:         "delete session resources",
			query:        deleteSessionResourcesQuery,
			arguments:    sessionLookupArguments(2, "sesn_test"),
			wantArgCount: 2,
		},
		{
			name:         "delete session events",
			query:        deleteSessionEventsQuery,
			arguments:    sessionLookupArguments(2, "sesn_test"),
			wantArgCount: 2,
		},
		{
			name:  "delete session event queue",
			query: deleteSessionEventQueueQuery,
			arguments: map[string]any{
				"organization_id": int64(1),
				"workspace_id":    int64(2),
				"session_uuid":    "11111111-1111-4111-8111-111111111111",
			},
			wantArgCount: 3,
		},
		{
			name:  "stop environment work",
			query: stopDeletedSessionEnvironmentWorkQuery,
			arguments: map[string]any{
				"workspace_id":            int64(2),
				"session_external_id":     "sesn_test",
				"environment_external_id": "env_test",
			},
			wantArgCount: 3,
		},
		{
			name:  "get resource",
			query: getSessionResourceQuery,
			arguments: map[string]any{
				"workspace_id":         int64(2),
				"session_external_id":  "sesn_test",
				"resource_external_id": "sesres_test",
			},
			wantArgCount: 3,
		},
		{
			name:  "update resource",
			query: updateSessionResourceQuery,
			arguments: map[string]any{
				"workspace_id":         int64(2),
				"session_external_id":  "sesn_test",
				"resource_external_id": "sesres_test",
				"payload":              []byte(`{}`),
				"secret_payload":       []byte(`{}`),
			},
			wantArgCount: 5,
		},
		{
			name:  "retire filesystem",
			query: retireSessionFilesystemQuery,
			arguments: map[string]any{
				"workspace_id":    int64(2),
				"organization_id": int64(1),
				"session_uuid":    "22222222-2222-4222-8222-222222222222",
				"retired_at":      now,
			},
			wantArgCount: 5,
		},
		{
			name:  "enqueue filesystem cleanup",
			query: enqueueFilestoreFilesystemCleanupJobQuery,
			arguments: map[string]any{
				"workspace_id":  int64(2),
				"filesystem_id": int64(4),
				"job_type":      filestoreFilesystemCleanupJobType,
				"run_after":     now,
			},
			wantArgCount: 4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bindNamed() error = %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("bound query retains named parameters: %q", query)
			}
			if strings.Contains(query, "::") {
				t.Fatalf("bound query contains PostgreSQL shorthand cast: %q", query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("bound argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}
