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
		AgentSnapshot: []byte(`{}`), Metadata: []byte(`{}`), VaultIDs: []byte(`[]`),
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
			statement: sessionMapperLockForMutationStatement,
			bound: buildSessionMapperLockForMutation(
				yourbatis.DialectPostgres,
				"workspace-uuid",
				"ses_test",
			),
			wantID:            "SessionMapper.LockForMutation",
			wantKind:          yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "sessionExternalID"},
			wantSQLFragments:  []string{"FROM sessions", "deleted_at IS NULL", "FOR UPDATE"},
		},
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

	fileBindingsBound := buildSessionResourceMapperListEventFileBindings(
		yourbatis.DialectPostgres,
		"workspace-uuid",
		"ses_test",
	)
	assertMapperSQLContains(t, fileBindingsBound, "file.external_id AS file_external_id")
	assertMapperSQLContains(t, fileBindingsBound, "resource.expires_at IS NULL OR resource.expires_at > now()")
	assertMapperSQLContains(t, fileBindingsBound, "ORDER BY resource.created_at ASC, resource.uuid ASC")

	fileReferenceBound := buildSessionEventMapperHasFileReferenceForResource(
		yourbatis.DialectPostgres,
		"workspace-uuid",
		"ses_test",
		"sesrsc_test",
	)
	assertMapperSQLContains(t, fileReferenceBound, "SELECT EXISTS")
	assertMapperSQLContains(t, fileReferenceBound, "resource.external_id = $1")
	assertMapperSQLContains(t, fileReferenceBound, "event.workspace_uuid = $2")
	assertMapperSQLContains(t, fileReferenceBound, "event.session_external_id = $3")
	assertMapperSQLContains(t, fileReferenceBound, "jsonb_array_elements")
	assertMapperSQLContains(t, fileReferenceBound, "event.event_type = 'user.message'")
	assertMapperSQLContains(t, fileReferenceBound, "content_block->'source'->>'file_id' = file.external_id")

	permissionRequestBound := buildSessionEventMapperFindLatestToolPermissionRequest(
		yourbatis.DialectPostgres,
		"workspace-uuid",
		"ses_test",
		"tool-use-id",
	)
	assertMapperSQLContains(t, permissionRequestBound, "event_type IN ('agent.tool_use', 'agent.mcp_tool_use')")
	assertMapperSQLContains(t, permissionRequestBound, "external_id = $3 OR payload->>'tool_use_id' = $4")
}

func TestSessionTableMappersPropagateExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []mapperExecutionErrorContract{
		{statementID: "SessionMapper.LockForMutation", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			mapper := NewSessionMapper(executor)
			_, err := mapper.LockForMutation(ctx, "", "")
			return err
		}},
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
		{statementID: "SessionEventMapper.HasFileReferenceForResource", kind: yourbatis.StatementSelect, query: true, call: func(executor yourbatis.Executor) error {
			mapper := NewSessionEventMapper(executor)
			_, err := mapper.HasFileReferenceForResource(ctx, "", "", "")
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.statementID, func(t *testing.T) { assertMapperExecutionError(t, test) })
	}
}
