package db

import (
	"context"
	"database/sql/driver"
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionInputMapperContracts(t *testing.T) {
	bound := buildCodeSessionMapperLockLatestForSession(yourbatis.DialectPostgres, "workspace", "session")
	assertMapperSQLContains(t, bound, "workspace_uuid = $1 AND session_uuid = $2")
	assertMapperSQLContains(t, bound, "ORDER BY created_at DESC, uuid DESC LIMIT 1 FOR UPDATE")
	if !reflect.DeepEqual(bound.Values(), []any{"workspace", "session"}) {
		t.Fatalf("input lock arguments: %v", bound.Values())
	}
	for _, keys := range [][]string{{"one"}, {"one", "two"}} {
		bound := buildCodeSessionMapperClearWorkerMetadata(yourbatis.DialectPostgres, "workspace", "worker", keys)
		assertMapperSQLContains(t, bound, "worker_external_metadata = worker_external_metadata - ARRAY[")
		assertMapperSQLContains(t, bound, "deleted_at IS NULL")
		expected := make([]any, 0, len(keys)+2)
		for _, key := range keys {
			expected = append(expected, key)
		}
		expected = append(expected, "workspace", "worker")
		if !reflect.DeepEqual(bound.Values(), expected) {
			t.Fatalf("metadata clear arguments: %v", bound.Values())
		}
	}
}

func TestPublicEventWorkerFenceIsScopedAndLocked(t *testing.T) {
	bound := buildCodeSessionMapperLockPublicEventWorker(yourbatis.DialectPostgres, "workspace", "session", "worker")
	assertMapperSQLContains(t, bound, "workspace_uuid = $1 AND session_uuid = $2 AND uuid = $3")
	assertMapperSQLContains(t, bound, "FOR UPDATE")
	if !reflect.DeepEqual(bound.Values(), []any{"workspace", "session", "worker"}) {
		t.Fatalf("worker fence arguments: %v", bound.Values())
	}
}

func TestCodeSessionMapperFindForSessionScope(t *testing.T) {
	bound := buildCodeSessionMapperFindForSession(yourbatis.DialectPostgres, "workspace", "session", "worker")
	assertMapperSQLContains(t, bound, "workspace_uuid = $1 AND session_uuid = $2 AND external_id = $3 AND deleted_at IS NULL")
	if !reflect.DeepEqual(bound.Values(), []any{"workspace", "session", "worker"}) {
		t.Fatalf("source worker arguments: %v", bound.Values())
	}
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
	_, found, err := NewCodeSessionMapper(executor).FindForSession(t.Context(), "workspace", "session", "missing")
	if err != nil || found {
		t.Fatalf("missing source worker: found=%v, error=%v", found, err)
	}
	assertMapperTestExecution(t, executor, "CodeSessionMapper.FindForSession", yourbatis.StatementSelect, []any{"workspace", "session", "missing"})
}

func TestCodeSessionMapperFindByExternalIDNotFound(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid"}})
	row, found, err := NewCodeSessionMapper(executor).FindByExternalID(context.Background(), "codeses_missing")
	if err != nil || found || row.UUID != "" {
		t.Fatalf("FindByExternalID() = (%+v, %t, %v), want zero, false, nil", row, found, err)
	}
	assertMapperTestExecution(
		t,
		executor,
		"CodeSessionMapper.FindByExternalID",
		yourbatis.StatementSelect,
		[]any{"codeses_missing"},
	)
}

func TestCodeSessionMapperFindVaultIDsNotFound(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"vault_ids"}})
	row, found, err := NewCodeSessionMapper(executor).FindVaultIDs(
		context.Background(),
		"org-uuid",
		"workspace-uuid",
		"codeses_missing",
	)
	if err != nil || found || len(row.VaultIDs) != 0 {
		t.Fatalf("FindVaultIDs() = (%+v, %t, %v), want zero, false, nil", row, found, err)
	}
	assertMapperTestExecution(
		t,
		executor,
		"CodeSessionMapper.FindVaultIDs",
		yourbatis.StatementSelect,
		[]any{"codeses_missing", "org-uuid", "workspace-uuid"},
	)
}

func TestCodeSessionMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	tokenHash := "token-hash"
	tokenSessionID := "token-session"
	epoch := int64(3)
	createParams := createCodeSessionParams{
		ExternalID: "codeses_test", OrganizationUUID: "org-uuid", WorkspaceUUID: "workspace-uuid",
		SessionUUID: "session-uuid", SessionExternalID: "session_test", EnvironmentUUID: "environment-uuid",
		EnvironmentExternalID: "env_test", WorkDir: "/workspace", PermissionMode: "default",
		Model: "model", Status: "active", Metadata: []byte(`{"source":"test"}`),
		OAuthAccessTokenHash: &tokenHash, InitialWorkerEpoch: 1, CreatedAt: now,
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: codeSessionMapperInsertStatement,
			bound:     buildCodeSessionMapperInsert(yourbatis.DialectPostgres, createParams),
			wantID:    "CodeSessionMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.SessionUUID",
				"params.SessionExternalID", "params.EnvironmentUUID", "params.EnvironmentExternalID", "params.WorkDir",
				"params.PermissionMode", "params.Model", "params.Status", "params.Metadata",
				"params.OAuthAccessTokenHash", "params.InitialWorkerEpoch",
				"params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.Metadata", "params.OAuthAccessTokenHash"},
			wantSQLFragments:           []string{"INSERT INTO code_sessions", "CAST($12 AS jsonb)", "RETURNING uuid"},
		}},
		{"credential lookup", mapperBuilderContract{
			statement: codeSessionMapperFindCredentialByOAuthAccessTokenHashStatement,
			bound: buildCodeSessionMapperFindCredentialByOAuthAccessTokenHash(
				yourbatis.DialectPostgres, tokenHash,
			),
			wantID: "CodeSessionMapper.FindCredentialByOAuthAccessTokenHash", wantKind: yourbatis.StatementSelect,
			wantArgumentNames:          []string{"tokenHash"},
			wantSensitiveArgumentNames: []string{"tokenHash"},
			wantSQLFragments:           []string{"JOIN sessions", "oauth_access_token_hash = $1", "worker_lease_expires_at > NOW()"},
		}},
		{"find active for environment work", mapperBuilderContract{
			statement: codeSessionMapperFindActiveForEnvironmentWorkStatement,
			bound: buildCodeSessionMapperFindActiveForEnvironmentWork(
				yourbatis.DialectPostgres, "org-uuid", "workspace-uuid", "environment-uuid", "session-uuid",
			),
			wantID: "CodeSessionMapper.FindActiveForEnvironmentWork", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "environmentUUID", "sessionUUID"},
			wantSQLFragments:  []string{"FROM code_sessions", "environment_uuid = $3", "session_uuid = $4", "LIMIT 2"},
		}},
		{"active ingress worker epoch", mapperBuilderContract{
			statement: codeSessionMapperCountActiveIngressWorkerEpochStatement,
			bound: buildCodeSessionMapperCountActiveIngressWorkerEpoch(
				yourbatis.DialectPostgres, "org-uuid", "workspace-uuid", "codeses_test", 1,
			),
			wantID: "CodeSessionMapper.CountActiveIngressWorkerEpoch", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"organizationUUID", "workspaceUUID", "codeSessionExternalID", "workerEpoch",
			},
			wantSQLFragments: []string{
				"SELECT COUNT(*)", "current_worker_epoch = $4", "status = 'active'",
			},
		}},
		{"network policy context", mapperBuilderContract{
			statement: codeSessionMapperFindNetworkPolicyContextStatement,
			bound: buildCodeSessionMapperFindNetworkPolicyContext(
				yourbatis.DialectPostgres, "org-uuid", "workspace-uuid", "codeses_test",
			),
			wantID: "CodeSessionMapper.FindNetworkPolicyContext", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"codeSessionExternalID", "organizationUUID", "workspaceUUID"},
			wantSQLFragments:  []string{"JOIN environments", "cs.external_id = $1", "cs.organization_uuid = $2"},
		}},
		{"register worker", mapperBuilderContract{
			statement: codeSessionMapperRegisterWorkerStatement,
			bound: buildCodeSessionMapperRegisterWorker(yourbatis.DialectPostgres, registerCodeSessionWorkerParams{
				UUID: "code-session-uuid", Epoch: epoch, ExpiresAt: expiresAt, Now: now,
				WorkerTokenSessionID: &tokenSessionID, WorkerBinding: []byte(`{"worker":"test"}`),
			}),
			wantID: "CodeSessionMapper.RegisterWorker", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.Epoch", "params.ExpiresAt", "params.Now", "params.WorkerTokenSessionID",
				"params.WorkerBinding", "params.Now", "params.Now", "params.Now", "params.UUID",
			},
			wantSensitiveArgumentNames: []string{"params.WorkerTokenSessionID", "params.WorkerBinding"},
			wantSQLFragments:           []string{"UPDATE code_sessions", "CAST($5 AS jsonb)", "RETURNING current_worker_epoch"},
		}},
		{"resume worker lease for sandbox", mapperBuilderContract{
			statement: codeSessionMapperResumeWorkerLeaseForSandboxStatement,
			bound: buildCodeSessionMapperResumeWorkerLeaseForSandbox(yourbatis.DialectPostgres, resumeCodeSessionWorkerLeaseParams{
				OrganizationUUID: "org-uuid", WorkspaceUUID: "workspace-uuid", CodeSessionExternalID: "codeses_test",
				ProviderSandboxID: "sandbox_test", ExpiresAt: expiresAt, Now: now,
			}),
			wantID: "CodeSessionMapper.ResumeWorkerLeaseForSandbox", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.ExpiresAt", "params.Now", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.CodeSessionExternalID", "params.ProviderSandboxID",
			},
			wantSQLFragments: []string{
				"UPDATE code_sessions", "current_worker_epoch > 0", "worker_lease_expires_at IS NOT NULL",
				"work.session_uuid = code_session.session_uuid", "JOIN environment_sandboxes",
				"provider_sandbox_id = $6", "work.state = 'active'",
			},
		}},
		{"update worker state", mapperBuilderContract{
			statement: codeSessionMapperUpdateWorkerStateStatement,
			bound: buildCodeSessionMapperUpdateWorkerState(yourbatis.DialectPostgres, updateCodeSessionWorkerStateParams{
				WorkspaceUUID: "workspace-uuid", UUID: "code-session-uuid", WorkerStatus: "running", RequiresActionDetails: []byte("null"),
				ExternalMetadata: []byte(`{"worker":"test"}`), Now: now,
			}),
			wantID: "CodeSessionMapper.UpdateWorkerState", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.WorkerStatus", "params.Now", "params.WorkerStatus", "params.RequiresActionDetails", "params.ExternalMetadata",
				"params.Now", "params.Now", "params.Now", "params.WorkspaceUUID", "params.UUID",
			},
			wantSensitiveArgumentNames: []string{"params.RequiresActionDetails", "params.ExternalMetadata"},
			wantSQLFragments:           []string{"worker_requires_action_details = CAST($4 AS jsonb)", "workspace_uuid = $9 AND uuid = $10", "deleted_at IS NULL", "RETURNING uuid"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("optional epoch predicates", func(t *testing.T) {
		withoutEpoch := buildCodeSessionMapperTouchWorkerActivity(
			yourbatis.DialectPostgres, "codeses_test", nil, now,
		)
		if containsMapperSQL(withoutEpoch.SQL, "current_worker_epoch") {
			t.Fatalf("touch SQL unexpectedly filters epoch: %q", withoutEpoch.SQL)
		}
		withEpoch := buildCodeSessionMapperTouchWorkerActivity(
			yourbatis.DialectPostgres, "codeses_test", &epoch, now,
		)
		assertMapperSQLContains(t, withEpoch, "current_worker_epoch = $4")

		connected := buildCodeSessionMapperUpdateConnection(yourbatis.DialectPostgres, updateCodeSessionConnectionParams{
			ExternalID: "codeses_test", Status: "connected", Connected: true, RequiredEpoch: &epoch, Now: now,
		})
		assertMapperSQLContains(t, connected, "last_worker_connected_at = $2")
		assertMapperSQLContains(t, connected, "current_worker_epoch = $6")

		disconnected := buildCodeSessionMapperUpdateConnection(yourbatis.DialectPostgres, updateCodeSessionConnectionParams{
			ExternalID: "codeses_test", Status: "disconnected", Now: now,
		})
		if containsMapperSQL(disconnected.SQL, "last_worker_connected_at") || containsMapperSQL(disconnected.SQL, "current_worker_epoch") {
			t.Fatalf("disconnect SQL contains optional fields: %q", disconnected.SQL)
		}
	})
}

func TestCodeSessionEventMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"worker stream", mapperBuilderContract{
			statement: codeSessionInboundEventMapperListForWorkerStreamStatement,
			bound: buildCodeSessionInboundEventMapperListForWorkerStream(
				yourbatis.DialectPostgres, "codeses_test", 2, 10,
			),
			wantID: "CodeSessionInboundEventMapper.ListForWorkerStream", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"codeSessionExternalID", "afterSequence", "epoch"},
			wantSQLFragments:  []string{"JOIN code_sessions", "e.sequence_num > $2", "cs.current_worker_epoch = $3"},
		}},
		{"delivery update", mapperBuilderContract{
			statement: codeSessionInboundEventMapperUpdateDeliveryStatement,
			bound: buildCodeSessionInboundEventMapperUpdateDelivery(yourbatis.DialectPostgres, updateCodeSessionInboundDeliveryParams{
				UUID: "event-uuid", TargetStatus: "processed", MarkReceived: true,
				MarkProcessing: true, MarkProcessed: true, Epoch: 2, Now: now,
			}),
			wantID: "CodeSessionInboundEventMapper.UpdateDelivery", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.TargetStatus", "params.MarkReceived", "params.Now", "params.MarkProcessing",
				"params.Now", "params.MarkProcessed", "params.Now", "params.Epoch", "params.Now",
				"params.Now", "params.UUID",
			},
			wantSQLFragments: []string{"UPDATE code_session_inbound_events", "delivery_worker_epoch = $8", "uuid = $11"},
		}},
		{"internal insert", mapperBuilderContract{
			statement: codeSessionInternalEventMapperInsertStatement,
			bound: buildCodeSessionInternalEventMapperInsert(yourbatis.DialectPostgres, codeSessionInternalEventInsertParams{
				ExternalID: "event_test", OrganizationUUID: "org-uuid", WorkspaceUUID: "workspace-uuid",
				CodeSessionUUID: "code-session-uuid", CodeSessionExternalID: "codeses_test", SequenceNum: 1,
				EventType: "assistant", PayloadUUID: "payload-uuid", Payload: []byte(`{"type":"assistant"}`),
				PayloadHash: "payload-hash", IdempotencyKey: "idem", EventMetadata: []byte(`{}`), CreatedAt: now,
			}),
			wantID: "CodeSessionInternalEventMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.CodeSessionUUID",
				"params.CodeSessionExternalID", "params.SequenceNum", "params.EventType", "params.PayloadUUID",
				"params.AgentID", "params.IsCompaction", "params.Payload", "params.PayloadHash",
				"params.IdempotencyKey", "params.EventMetadata", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.Payload", "params.PayloadHash", "params.EventMetadata"},
			wantSQLFragments:           []string{"INSERT INTO code_session_internal_events", "ON CONFLICT (workspace_uuid, idempotency_key)", "RETURNING uuid"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestCodeSessionInternalEventMapperBuildsScopePages(t *testing.T) {
	for _, subagents := range []bool{false, true} {
		bound := buildCodeSessionInternalEventMapperListPage(yourbatis.DialectPostgres, listCodeSessionInternalEventsParams{
			WorkspaceUUID: "workspace-uuid", CodeSessionExternalID: "codeses_test",
			Subagents: subagents, AfterSequence: 10, Limit: 501,
		})
		assertMapperArgumentNames(t, bound, []string{
			"params.WorkspaceUUID", "params.CodeSessionExternalID", "params.Subagents", "params.Subagents",
			"params.WorkspaceUUID", "params.CodeSessionExternalID", "params.Subagents", "params.Subagents",
			"params.AfterSequence", "params.Limit",
		})
		assertMapperSQLContains(t, bound, "b.agent_id IS NOT DISTINCT FROM e.agent_id")
		assertMapperSQLContains(t, bound, "GREATEST( CAST($9 AS bigint), COALESCE(b.sequence_num - 1, 0) )")
	}
}

func TestInternalTranscriptMaterializationDoesNotApplyResumeCompaction(t *testing.T) {
	bound := buildCodeSessionInternalEventMapperListForPublicEvents(yourbatis.DialectPostgres, "workspace", "worker", 12, 500)
	assertMapperSQLContains(t, bound, "workspace_uuid = $1 AND code_session_external_id = $2")
	assertMapperSQLContains(t, bound, "agent_id IS NOT NULL AND deleted_at IS NULL AND sequence_num > $3 ORDER BY sequence_num ASC LIMIT $4")
	if !reflect.DeepEqual(bound.Values(), []any{"workspace", "worker", int64(12), 500}) {
		t.Fatalf("materialization query arguments: %v", bound.Values())
	}
}

func TestWorkerPermissionMetadataMergeIsScopedAndSensitive(t *testing.T) {
	metadata := []byte(`{"request":{"input":{"value":9007199254740993}}}`)
	bound := buildCodeSessionMapperMergeWorkerMetadata(yourbatis.DialectPostgres, "workspace", "worker", metadata)
	assertMapperSQLContains(t, bound, "COALESCE(worker_external_metadata, CAST('{}' AS jsonb)) || CAST($1 AS jsonb)")
	assertMapperSQLContains(t, bound, "workspace_uuid = $2 AND uuid = $3 AND deleted_at IS NULL")
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: codeSessionMapperMergeWorkerMetadataStatement, bound: bound,
		wantID: "CodeSessionMapper.MergeWorkerMetadata", wantKind: yourbatis.StatementUpdate,
		wantArgumentNames:          []string{"metadata", "workspaceUUID", "codeSessionUUID"},
		wantSensitiveArgumentNames: []string{"metadata"},
	})
}

func TestInternalEventRetryMatchesContentAndOwnership(t *testing.T) {
	params := codeSessionInternalEventInsertParams{
		WorkspaceUUID: "workspace", CodeSessionUUID: "worker", IdempotencyKey: "retry",
		EventType: "assistant", PayloadUUID: "source", AgentID: new("child"), IsCompaction: true,
		Payload: []byte(`{"value":9007199254740993}`), EventMetadata: []byte(`{"batch":1}`),
	}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: codeSessionInternalEventMapperMatchesRetryStatement,
		bound:     buildCodeSessionInternalEventMapperMatchesRetry(yourbatis.DialectPostgres, params),
		wantID:    "CodeSessionInternalEventMapper.MatchesRetry", wantKind: yourbatis.StatementSelect,
		wantArgumentNames:          []string{"params.WorkspaceUUID", "params.IdempotencyKey", "params.CodeSessionUUID", "params.EventType", "params.PayloadUUID", "params.AgentID", "params.IsCompaction", "params.Payload", "params.EventMetadata"},
		wantSensitiveArgumentNames: []string{"params.Payload", "params.EventMetadata"},
		wantSQLFragments:           []string{"SELECT EXISTS", "workspace_uuid = $1", "idempotency_key = $2", "deleted_at IS NULL", "code_session_uuid = $3", "event_type = $4", "payload_uuid = $5", "agent_id IS NOT DISTINCT FROM $6", "is_compaction = $7", "payload = CAST($8 AS jsonb)", "event_metadata IS NOT DISTINCT FROM CAST($9 AS jsonb)"},
	})
	for _, matches := range []bool{false, true} {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"matches"}, rows: [][]driver.Value{{matches}}})
		got, err := NewCodeSessionInternalEventMapper(executor).MatchesRetry(t.Context(), params)
		if err != nil || got != matches {
			t.Fatalf("MatchesRetry = %t, %v; want %t", got, err, matches)
		}
	}
}
