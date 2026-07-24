package db

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimeResourceQueriesUseSQLXNamedParameters(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 10, 30, 0, 0, time.UTC)
	resource := SessionResource{
		UUID:              "00000000-0000-0000-0000-000000000123",
		ExternalID:        "session_resource_test",
		OrganizationID:    7,
		WorkspaceID:       42,
		SessionExternalID: "session_test",
		ResourceType:      "file",
		Payload:           json.RawMessage(`{"source":"/uploads"}`),
		SecretPayload:     json.RawMessage(`{}`),
		CreatedAt:         createdAt,
	}
	createArguments := map[string]any{
		"resource_uuid":        resource.UUID,
		"resource_external_id": resource.ExternalID,
		"organization_id":      resource.OrganizationID,
		"workspace_id":         resource.WorkspaceID,
		"session_external_id":  resource.SessionExternalID,
		"resource_type":        resource.ResourceType,
		"payload":              jsonArg(resource.Payload),
		"secret_payload":       jsonArg(resource.SecretPayload),
		"created_at":           resource.CreatedAt,
	}

	tests := []struct {
		name      string
		query     string
		arguments map[string]any
		want      []any
	}{
		{
			name:      "get file",
			query:     getFileQuery,
			arguments: getFileArguments(42, "file_test"),
			want:      []any{int64(42), "file_test"},
		},
		{
			name:      "get session",
			query:     getSessionQuery,
			arguments: sessionLookupArguments(42, "session_test"),
			want:      []any{int64(42), "session_test"},
		},
		{
			name:      "list session resources",
			query:     listSessionResourcesQuery,
			arguments: sessionLookupArguments(42, "session_test"),
			want:      []any{int64(42), "session_test"},
		},
		{
			name:      "create session resource",
			query:     createSessionResourceQuery,
			arguments: createArguments,
			want: []any{
				resource.UUID,
				resource.ExternalID,
				resource.OrganizationID,
				resource.WorkspaceID,
				resource.SessionExternalID,
				resource.ResourceType,
				[]byte(resource.Payload),
				[]byte(resource.SecretPayload),
				resource.CreatedAt,
				resource.CreatedAt,
				resource.WorkspaceID,
				resource.SessionExternalID,
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
				t.Fatalf("query retains colon syntax after binding: %q", query)
			}
			if !reflect.DeepEqual(arguments, test.want) {
				t.Fatalf("arguments = %#v, want %#v", arguments, test.want)
			}
		})
	}
}

func TestSessionCreationQueriesUseSQLXNamedParameters(t *testing.T) {
	createdAt := time.Date(2026, time.July, 23, 11, 0, 0, 0, time.UTC)
	session := Session{
		UUID:                  "00000000-0000-0000-0000-000000000111",
		ExternalID:            "session_test",
		OrganizationID:        7,
		WorkspaceID:           42,
		CreatedByAPIKeyID:     9,
		EnvironmentID:         10,
		EnvironmentExternalID: "environment_test",
		AgentID:               11,
		AgentExternalID:       "agent_test",
		AgentVersion:          1,
		AgentSnapshot:         json.RawMessage(`{}`),
		Metadata:              json.RawMessage(`{}`),
		VaultIDs:              json.RawMessage(`[]`),
		Status:                "idle",
		Usage:                 json.RawMessage(`{}`),
		Stats:                 json.RawMessage(`{}`),
		OutcomeEvaluations:    json.RawMessage(`[]`),
		CreatedAt:             createdAt,
	}
	thread := SessionThread{
		UUID:              "00000000-0000-0000-0000-000000000222",
		ExternalID:        "session_thread_test",
		OrganizationID:    7,
		WorkspaceID:       42,
		SessionID:         12,
		SessionExternalID: session.ExternalID,
		AgentSnapshot:     json.RawMessage(`{}`),
		Status:            "idle",
		Usage:             json.RawMessage(`{}`),
		Stats:             json.RawMessage(`{}`),
		CreatedAt:         createdAt,
	}
	work := EnvironmentWork{
		UUID:                  "00000000-0000-0000-0000-000000000333",
		ExternalID:            "environment_work_test",
		OrganizationID:        7,
		WorkspaceID:           42,
		EnvironmentID:         10,
		EnvironmentExternalID: "environment_test",
		Data:                  json.RawMessage(`{}`),
		Metadata:              json.RawMessage(`{}`),
		State:                 "pending",
		CreatedAt:             createdAt,
	}

	tests := []struct {
		name         string
		query        string
		arguments    map[string]any
		wantArgCount int
	}{
		{"session", createSessionQuery, createSessionArguments(session), 21},
		{"filesystem", insertSessionFilesystemSQLXQuery, sessionFilesystemArguments(session, "claude_chat_test", createdAt), 7},
		{"filesystem conflict", sessionFilesystemExternalIDConflictQuery, sessionFilesystemArguments(session, "claude_chat_test", createdAt), 2},
		{"thread", createSessionThreadQuery, createSessionThreadArguments(thread), 14},
		{"environment work", createEnvironmentWorkQuery, createEnvironmentWorkArguments(work), 12},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query, arguments, err := bindNamed(postgresRebinder{}, test.query, test.arguments)
			if err != nil {
				t.Fatalf("bind named query: %v", err)
			}
			if strings.Contains(query, ":") {
				t.Fatalf("query retains colon syntax after binding: %q", query)
			}
			if len(arguments) != test.wantArgCount {
				t.Fatalf("argument count = %d, want %d", len(arguments), test.wantArgCount)
			}
		})
	}
}
