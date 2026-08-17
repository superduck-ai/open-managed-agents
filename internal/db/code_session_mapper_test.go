package db

import (
	"context"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

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
		OAuthAccessTokenHash: &tokenHash, CreatedAt: now,
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
				"params.OAuthAccessTokenHash", "params.CreatedAt", "params.CreatedAt",
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
		{"update worker state", mapperBuilderContract{
			statement: codeSessionMapperUpdateWorkerStateStatement,
			bound: buildCodeSessionMapperUpdateWorkerState(yourbatis.DialectPostgres, updateCodeSessionWorkerStateParams{
				UUID: "code-session-uuid", WorkerStatus: "running", RequiresActionDetails: []byte("null"),
				ExternalMetadata: []byte(`{"worker":"test"}`), Now: now,
			}),
			wantID: "CodeSessionMapper.UpdateWorkerState", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.WorkerStatus", "params.RequiresActionDetails", "params.ExternalMetadata",
				"params.Now", "params.Now", "params.Now", "params.UUID",
			},
			wantSensitiveArgumentNames: []string{"params.RequiresActionDetails", "params.ExternalMetadata"},
			wantSQLFragments:           []string{"worker_requires_action_details = CAST($2 AS jsonb)", "RETURNING uuid"},
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
