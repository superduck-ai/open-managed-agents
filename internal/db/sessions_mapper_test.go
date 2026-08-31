package db

import (
	"context"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionMapperFindByExternalIDNotFound(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
	row, found, err := NewSessionMapper(executor).FindByExternalID(
		context.Background(),
		"workspace-uuid",
		"ses_missing",
	)
	if err != nil || found || row.UUID != "" {
		t.Fatalf("FindByExternalID() = (%+v, %t, %v), want zero, false, nil", row, found, err)
	}
	assertMapperTestExecution(
		t,
		executor,
		"SessionMapper.FindByExternalID",
		yourbatis.StatementSelect,
		[]any{"workspace-uuid", "ses_missing"},
	)
}

func TestSessionTableMapperWriteBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	sessionParams := sessionWriteParams{
		UUID: "session-uuid", ExternalID: "ses_test", OrganizationUUID: "organization-uuid",
		WorkspaceUUID: "workspace-uuid", CreatedByAPIKeyUUID: "api-key-uuid",
		EnvironmentUUID: "environment-uuid", EnvironmentExternalID: "env_test",
		AgentUUID: "agent-uuid", AgentExternalID: "agent_test", AgentVersion: 1,
		AgentSnapshot: []byte(`{}`), Metadata: []byte(`{}`), VaultIDs: sessionVaultIDs{},
		Status: "idle", Usage: []byte(`{}`), Stats: []byte(`{}`), OutcomeEvaluations: []byte(`[]`),
		CreatedAt: now,
	}
	threadParams := sessionThreadWriteParams{
		UUID: "thread-uuid", ExternalID: "thread_test", OrganizationUUID: "organization-uuid",
		WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", SessionExternalID: "ses_test",
		AgentSnapshot: []byte(`{}`), Status: "idle", Usage: []byte(`{}`), Stats: []byte(`{}`),
		CreatedAt: now,
	}
	resourceParams := sessionResourceWriteParams{
		UUID: "resource-uuid", ExternalID: "resource_test", OrganizationUUID: "organization-uuid",
		WorkspaceUUID: "workspace-uuid", SessionExternalID: "ses_test", ResourceType: "file",
		Payload: []byte(`{"file_id":"file_test"}`), SecretPayload: []byte(`{}`), CreatedAt: now,
	}
	eventParams := sessionEventWriteParams{
		UUID: "event-uuid", ExternalID: "event_test", OrganizationUUID: "organization-uuid",
		WorkspaceUUID: "workspace-uuid", SessionUUID: "session-uuid", SessionExternalID: "ses_test",
		EventType: "message", Payload: []byte(`{"type":"message"}`), ProcessedAt: now, CreatedAt: now,
	}

	tests := []mapperBuilderContract{
		{
			statement: sessionMapperInsertStatement,
			bound:     buildSessionMapperInsert(yourbatis.DialectPostgres, sessionParams),
			wantID:    "SessionMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.CreatedByAPIKeyUUID", "params.EnvironmentUUID", "params.EnvironmentExternalID",
				"params.AgentUUID", "params.AgentExternalID", "params.AgentVersion", "params.AgentSnapshot",
				"params.DeploymentUUID", "params.DeploymentID", "params.Title", "params.Metadata",
				"params.VaultIDs", "params.Status", "params.Usage", "params.Stats",
				"params.OutcomeEvaluations", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{
				"params.AgentSnapshot", "params.Metadata", "params.VaultIDs", "params.Usage",
				"params.Stats", "params.OutcomeEvaluations",
			},
			wantSQLFragments: []string{"INSERT INTO sessions", "CAST($11 AS jsonb)", "RETURNING", "uuid, external_id"},
		},
		{
			statement: sessionMapperFindByUUIDStatement,
			bound:     buildSessionMapperFindByUUID(yourbatis.DialectPostgres, "workspace-uuid", "session-uuid"),
			wantID:    "SessionMapper.FindByUUID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "sessionUUID"},
			wantSQLFragments:  []string{"FROM sessions", "workspace_uuid = $1", "uuid = $2", "deleted_at IS NULL"},
		},
		{
			statement: sessionThreadMapperInsertIfAbsentStatement,
			bound:     buildSessionThreadMapperInsertIfAbsent(yourbatis.DialectPostgres, threadParams),
			wantID:    "SessionThreadMapper.InsertIfAbsent", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.SessionUUID", "params.SessionExternalID", "params.ParentThreadUUID",
				"params.ParentThreadExternalID", "params.AgentSnapshot", "params.Status",
				"params.Usage", "params.Stats", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.AgentSnapshot", "params.Usage", "params.Stats"},
			wantSQLFragments:           []string{"WITH inserted AS", "ON CONFLICT", "FROM inserted"},
		},
		{
			statement: sessionResourceMapperInsertStatement,
			bound:     buildSessionResourceMapperInsert(yourbatis.DialectPostgres, resourceParams),
			wantID:    "SessionResourceMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.SessionExternalID", "params.ResourceType", "params.Payload", "params.SecretPayload",
				"params.CreatedAt", "params.CreatedAt", "params.WorkspaceUUID", "params.SessionExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.Payload", "params.SecretPayload"},
			wantSQLFragments:           []string{"INSERT INTO session_resources", "FROM sessions s", "RETURNING"},
		},
		{
			statement: sessionEventMapperInsertIfAbsentStatement,
			bound:     buildSessionEventMapperInsertIfAbsent(yourbatis.DialectPostgres, eventParams),
			wantID:    "SessionEventMapper.InsertIfAbsent", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.SessionUUID", "params.SessionExternalID", "params.ThreadUUID",
				"params.ThreadExternalID", "params.EventType", "params.Payload",
				"params.ProcessedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.Payload"},
			wantSQLFragments:           []string{"WITH inserted AS", "INSERT INTO session_events", "ON CONFLICT"},
		},
	}

	for _, test := range tests {
		t.Run(test.wantID, func(t *testing.T) { assertMapperBuilderContract(t, test) })
	}
}

func TestSessionVaultIDsJSONBoundary(t *testing.T) {
	t.Run("rejects invalid JSON", func(t *testing.T) {
		var ids sessionVaultIDs
		if err := ids.Scan([]byte(`{"not":"an array"}`)); err == nil {
			t.Fatal("Scan() error = nil")
		}
	})

	t.Run("rejects unsupported source", func(t *testing.T) {
		var ids sessionVaultIDs
		if err := ids.Scan(42); err == nil {
			t.Fatal("Scan() error = nil")
		}
	})

	t.Run("scans JSON array", func(t *testing.T) {
		var ids sessionVaultIDs
		if err := ids.Scan([]byte(`["vlt_one","vlt_two"]`)); err != nil {
			t.Fatalf("Scan(): %v", err)
		}
		if len(ids) != 2 || ids[0] != "vlt_one" || ids[1] != "vlt_two" {
			t.Fatalf("Scan() = %v", ids)
		}
	})

	t.Run("canonicalizes null", func(t *testing.T) {
		var ids sessionVaultIDs
		if err := ids.Scan(nil); err != nil {
			t.Fatalf("Scan(): %v", err)
		}
		if ids == nil || len(ids) != 0 {
			t.Fatalf("Scan() = %#v, want non-nil empty slice", ids)
		}
	})

	t.Run("writes JSON array", func(t *testing.T) {
		value, err := (sessionVaultIDs{"vlt_one", "vlt_two"}).Value()
		if err != nil {
			t.Fatalf("Value(): %v", err)
		}
		raw, ok := value.([]byte)
		if !ok || string(raw) != `["vlt_one","vlt_two"]` {
			t.Fatalf("Value() = %#v", value)
		}
	})
}

func TestSessionTableMappersBuildDynamicPages(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	agentVersion := 2
	sessionBound := buildSessionMapperListPage(yourbatis.DialectPostgres, sessionPageMapperParams{
		WorkspaceUUID: "workspace-uuid", FetchLimit: 21,
		Cursor: &SessionPageCursor{CreatedAt: now, UUID: "session-uuid"}, Descending: true,
		AgentExternalID: "agent_test", AgentVersion: &agentVersion, DeploymentID: "deployment_test",
		MemoryStoreID: "memory_test", Statuses: []string{"idle", "running"}, CreatedAtGTE: &now,
	})
	assertMapperSQLContains(t, sessionBound, "s.status IN ( $7 , $8 )")
	assertMapperSQLContains(t, sessionBound, "(s.created_at, s.uuid) < ($10, $11)")
	assertMapperSQLContains(t, sessionBound, "ORDER BY s.created_at DESC, s.uuid DESC")

	eventBound := buildSessionEventMapperListPage(yourbatis.DialectPostgres, sessionEventPageMapperParams{
		WorkspaceUUID: "workspace-uuid", SessionExternalID: "ses_test", PrimaryOnly: true,
		FetchLimit: 21, Cursor: &SessionEventPageCursor{CreatedAt: now, UUID: "event-uuid"},
		Types: []string{"message", "result"},
	})
	assertMapperSQLContains(t, eventBound, "parent_thread_uuid IS NULL")
	assertMapperSQLContains(t, eventBound, "event_type IN ( $5 , $6 )")
	assertMapperSQLContains(t, eventBound, "ORDER BY created_at ASC, uuid ASC")

	toolUseBound := buildSessionEventMapperChildSessionToolUseIDs(
		yourbatis.DialectPostgres,
		"workspace-uuid",
		"ses_test",
		[]string{"agent.tool_use", "agent.mcp_tool_use"},
		[]string{"tool-1", "tool-2"},
	)
	assertMapperSQLContains(t, toolUseBound, "e.event_type IN ( $3 , $4 )")
	assertMapperSQLContains(t, toolUseBound, ") IN ( $5 , $6 )")

}

func TestSessionTableMappersPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []mapperExecutionErrorContract{
		{statementID: "SessionMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			mapper := NewSessionMapper(executor)
			_, err := mapper.Insert(ctx, sessionWriteParams{})
			return err
		}},
		{statementID: "SessionMapper.SetStatus", kind: yourbatis.StatementUpdate, call: func(executor yourbatis.Executor) error {
			mapper := NewSessionMapper(executor)
			_, err := mapper.SetStatus(ctx, "", "", "")
			return err
		}},
		{statementID: "SessionThreadMapper.InsertIfAbsent", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			mapper := NewSessionThreadMapper(executor)
			_, _, err := mapper.InsertIfAbsent(ctx, sessionThreadWriteParams{})
			return err
		}},
		{statementID: "SessionResourceMapper.Insert", kind: yourbatis.StatementInsert, query: true, call: func(executor yourbatis.Executor) error {
			mapper := NewSessionResourceMapper(executor)
			_, err := mapper.Insert(ctx, sessionResourceWriteParams{})
			return err
		}},
		{statementID: "SessionEventMapper.InsertIfAbsent", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			mapper := NewSessionEventMapper(executor)
			_, _, err := mapper.InsertIfAbsent(ctx, sessionEventWriteParams{})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.statementID, func(t *testing.T) { assertMapperExecutionError(t, test) })
	}
}
