package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestEnvironmentMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := environmentMapperTestWriteParams(now)
	page := environmentPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, FetchLimit: 21,
		Cursor: &EnvironmentPageCursor{CreatedAt: now, UUID: "00000000-0000-4000-8000-000000000009"},
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: environmentMapperInsertStatement, bound: buildEnvironmentMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "EnvironmentMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.CreatedByAPIKeyUUID", "params.Name", "params.Description", "params.Config", "params.Metadata",
				"params.Scope", "params.Provider", "params.ResolvedTemplate", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.Config", "params.Metadata"},
			wantSQLFragments:           []string{"INSERT INTO environments", "CAST($8 AS jsonb)", "RETURNING"},
		}},
		{"find", mapperBuilderContract{
			statement: environmentMapperFindByExternalIDStatement,
			bound:     buildEnvironmentMapperFindByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "EnvironmentMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"FROM environments", "external_id = $2"},
		}},
		{"find by UUID", mapperBuilderContract{
			statement: environmentMapperFindByUUIDStatement,
			bound:     buildEnvironmentMapperFindByUUID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.UUID),
			wantID:    "EnvironmentMapper.FindByUUID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "environmentUUID"}, wantSQLFragments: []string{"uuid = $2"},
		}},
		{"update", mapperBuilderContract{
			statement: environmentMapperUpdateByExternalIDStatement,
			bound:     buildEnvironmentMapperUpdateByExternalID(yourbatis.DialectPostgres, params),
			wantID:    "EnvironmentMapper.UpdateByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.Name", "params.Description", "params.Config", "params.Metadata", "params.Scope",
				"params.ResolvedTemplate", "params.UpdatedAt", "params.WorkspaceUUID", "params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.Config", "params.Metadata"},
			wantSQLFragments:           []string{"UPDATE environments", "workspace_uuid = $8", "RETURNING"},
		}},
		{"archive", mapperBuilderContract{
			statement: environmentMapperArchiveByExternalIDStatement,
			bound:     buildEnvironmentMapperArchiveByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "EnvironmentMapper.ArchiveByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"archived_at = COALESCE", "RETURNING"},
		}},
		{"lock UUID", mapperBuilderContract{
			statement: environmentMapperLockUUIDByExternalIDStatement,
			bound:     buildEnvironmentMapperLockUUIDByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID:    "EnvironmentMapper.LockUUIDByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"SELECT uuid", "FOR UPDATE"},
		}},
		{"soft delete", mapperBuilderContract{
			statement: environmentMapperSoftDeleteByUUIDStatement,
			bound:     buildEnvironmentMapperSoftDeleteByUUID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.UUID),
			wantID:    "EnvironmentMapper.SoftDeleteByUUID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "environmentUUID"}, wantSQLFragments: []string{"deleted_at = COALESCE", "uuid = $2"},
		}},
		{"list page", mapperBuilderContract{
			statement: environmentMapperListPageStatement, bound: buildEnvironmentMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "EnvironmentMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.Cursor.CreatedAt", "params.Cursor.UUID", "params.FetchLimit"},
			wantSQLFragments:  []string{"archived_at IS NULL", "(created_at, uuid) < ($2, $3)", "LIMIT $4"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestEnvironmentKeyAndWorkerPollMapperBuilderContracts(t *testing.T) {
	params := environmentKeyUpsertParams{
		ExternalID: "envkey_test", OrganizationUUID: "00000000-0000-4000-8000-000000000001",
		WorkspaceUUID:         "00000000-0000-4000-8000-000000000002",
		EnvironmentUUID:       "00000000-0000-4000-8000-000000000003",
		EnvironmentExternalID: "env_test", KeyHash: "hash",
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"upsert key", mapperBuilderContract{
			statement: environmentKeyMapperUpsertStatement, bound: buildEnvironmentKeyMapperUpsert(yourbatis.DialectPostgres, params),
			wantID: "EnvironmentKeyMapper.Upsert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.EnvironmentUUID", "params.EnvironmentExternalID", "params.KeyHash",
			},
			wantSensitiveArgumentNames: []string{"params.KeyHash"},
			wantSQLFragments:           []string{"INSERT INTO environment_keys", "ON CONFLICT", "key_hash = EXCLUDED.key_hash"},
		}},
		{"find key", mapperBuilderContract{
			statement: environmentKeyMapperFindAndTouchByHashStatement,
			bound:     buildEnvironmentKeyMapperFindAndTouchByHash(yourbatis.DialectPostgres, params.KeyHash),
			wantID:    "EnvironmentKeyMapper.FindAndTouchByHash", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"keyHash"}, wantSensitiveArgumentNames: []string{"keyHash"},
			wantSQLFragments: []string{"UPDATE environment_keys", "JOIN workspaces", "workspace_external_id"},
		}},
		{"record poll", mapperBuilderContract{
			statement: environmentWorkerPollMapperUpsertStatement,
			bound:     buildEnvironmentWorkerPollMapperUpsert(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID, "worker"),
			wantID:    "EnvironmentWorkerPollMapper.Upsert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{"workerID", "workspaceUUID", "environmentExternalID"},
			wantSQLFragments:  []string{"INSERT INTO environment_worker_polls", "FROM environments", "ON CONFLICT"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestEnvironmentWorkMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := environmentWorkMapperTestWriteParams(now)
	workerID := "worker"
	metadataParams := environmentWorkMetadataParams{
		WorkspaceUUID: params.WorkspaceUUID, EnvironmentExternalID: params.EnvironmentExternalID,
		WorkExternalID: params.ExternalID, Metadata: []byte(`{}`),
	}
	heartbeatParams := environmentWorkHeartbeatParams{
		WorkspaceUUID: params.WorkspaceUUID, EnvironmentExternalID: params.EnvironmentExternalID,
		WorkUUID: params.UUID, State: "active", TTLSeconds: 60,
	}
	recoveryParams := environmentWorkRecoveryRetryParams{
		OrganizationUUID: params.OrganizationUUID, WorkspaceUUID: params.WorkspaceUUID,
		EnvironmentUUID: params.EnvironmentUUID, WorkUUID: params.UUID, RetryAt: now,
	}
	stopParams := environmentWorkStopParams{
		WorkspaceUUID: params.WorkspaceUUID, EnvironmentExternalID: params.EnvironmentExternalID,
		WorkExternalID: params.ExternalID, State: "stopped",
	}
	page := environmentWorkPageMapperParams{
		WorkspaceUUID: params.WorkspaceUUID, EnvironmentExternalID: params.EnvironmentExternalID, FetchLimit: 21,
		Cursor: &EnvironmentWorkPageCursor{CreatedAt: now, UUID: "00000000-0000-4000-8000-000000000009"},
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: environmentWorkMapperInsertStatement, bound: buildEnvironmentWorkMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "EnvironmentWorkMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.EnvironmentUUID", "params.EnvironmentExternalID", "params.Data", "params.Metadata",
				"params.Secret", "params.State", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.Data", "params.Metadata", "params.Secret"},
			wantSQLFragments:           []string{"INSERT INTO environment_work", "CAST($7 AS jsonb)", "RETURNING"},
		}},
		{"count active", mapperBuilderContract{
			statement: environmentWorkMapperCountActiveStatement,
			bound:     buildEnvironmentWorkMapperCountActive(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentUUID),
			wantID:    "EnvironmentWorkMapper.CountActive", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "environmentUUID"}, wantSQLFragments: []string{"SELECT COUNT(*)", "state <> 'stopped'"},
		}},
		{"find", mapperBuilderContract{
			statement: environmentWorkMapperFindByExternalIDStatement,
			bound:     buildEnvironmentWorkMapperFindByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID, params.ExternalID),
			wantID:    "EnvironmentWorkMapper.FindByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "environmentExternalID", "workExternalID"}, wantSQLFragments: []string{"FROM environment_work", "external_id = $3"},
		}},
		{"find latest", mapperBuilderContract{
			statement: environmentWorkMapperFindLatestByDataStatement,
			bound:     buildEnvironmentWorkMapperFindLatestByData(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID, "session", "ses_test"),
			wantID:    "EnvironmentWorkMapper.FindLatestByData", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "environmentExternalID", "dataType", "dataID"},
			wantSQLFragments:  []string{"data->>'type' = $3", "ORDER BY created_at DESC"},
		}},
		{"list page", mapperBuilderContract{
			statement: environmentWorkMapperListPageStatement, bound: buildEnvironmentWorkMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "EnvironmentWorkMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{
				"params.WorkspaceUUID", "params.EnvironmentExternalID", "params.Cursor.CreatedAt",
				"params.Cursor.UUID", "params.FetchLimit",
			},
			wantSQLFragments: []string{"(created_at, uuid) < ($3, $4)", "LIMIT $5"},
		}},
		{"claim for environment", mapperBuilderContract{
			statement: environmentWorkMapperClaimForEnvironmentStatement,
			bound:     buildEnvironmentWorkMapperClaimForEnvironment(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID, &workerID, now),
			wantID:    "EnvironmentWorkMapper.ClaimForEnvironment", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workerID", "claimExpiresAt", "workspaceUUID", "environmentExternalID"},
			wantSQLFragments:  []string{"FOR UPDATE SKIP LOCKED", "RETURNING"},
		}},
		{"claim next", mapperBuilderContract{
			statement: environmentWorkMapperClaimNextStatement,
			bound:     buildEnvironmentWorkMapperClaimNext(yourbatis.DialectPostgres, &workerID, now, false),
			wantID:    "EnvironmentWorkMapper.ClaimNext", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workerID", "claimExpiresAt"},
			wantSQLFragments:  []string{"COALESCE(data->>'type', '') <> 'session'", "FOR UPDATE SKIP LOCKED"},
		}},
		{"lock", mapperBuilderContract{
			statement: environmentWorkMapperLockByExternalIDStatement,
			bound:     buildEnvironmentWorkMapperLockByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID, params.ExternalID),
			wantID:    "EnvironmentWorkMapper.LockByExternalID", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "environmentExternalID", "workExternalID"}, wantSQLFragments: []string{"FOR UPDATE"},
		}},
		{"ack", mapperBuilderContract{
			statement: environmentWorkMapperAckByExternalIDStatement,
			bound:     buildEnvironmentWorkMapperAckByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID, params.ExternalID),
			wantID:    "EnvironmentWorkMapper.AckByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "environmentExternalID", "workExternalID"}, wantSQLFragments: []string{"acknowledged_at = COALESCE", "RETURNING"},
		}},
		{"metadata", mapperBuilderContract{
			statement: environmentWorkMapperUpdateMetadataStatement,
			bound:     buildEnvironmentWorkMapperUpdateMetadata(yourbatis.DialectPostgres, metadataParams),
			wantID:    "EnvironmentWorkMapper.UpdateMetadata", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.Metadata", "params.WorkspaceUUID", "params.EnvironmentExternalID", "params.WorkExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.Metadata"}, wantSQLFragments: []string{"CAST($1 AS jsonb)", "RETURNING"},
		}},
		{"requeue recoverable work", mapperBuilderContract{
			statement: environmentWorkMapperRequeueIfRecoverableStatement,
			bound:     buildEnvironmentWorkMapperRequeueIfRecoverable(yourbatis.DialectPostgres, recoveryParams),
			wantID:    "EnvironmentWorkMapper.RequeueIfRecoverable", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.RetryAt", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.EnvironmentUUID", "params.WorkUUID",
			},
			wantSQLFragments: []string{
				"state = 'queued'", "claim_expires_at = $1", "state IN ('starting', 'active')",
				"EXISTS", "code_session.session_external_id = environment_work.data->>'id'",
			},
		}},
		{"heartbeat", mapperBuilderContract{
			statement: environmentWorkMapperHeartbeatStatement,
			bound:     buildEnvironmentWorkMapperHeartbeat(yourbatis.DialectPostgres, heartbeatParams),
			wantID:    "EnvironmentWorkMapper.Heartbeat", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.State", "params.TTLSeconds", "params.WorkUUID", "params.WorkspaceUUID", "params.EnvironmentExternalID",
			},
			wantSQLFragments: []string{"latest_heartbeat_at = NOW()", "uuid = $3", "RETURNING"},
		}},
		{"stop", mapperBuilderContract{
			statement: environmentWorkMapperStopStatement, bound: buildEnvironmentWorkMapperStop(yourbatis.DialectPostgres, stopParams),
			wantID: "EnvironmentWorkMapper.Stop", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.State", "params.State", "params.WorkspaceUUID", "params.EnvironmentExternalID", "params.WorkExternalID",
			},
			wantSQLFragments: []string{"WHEN $2 = 'stopped'", "RETURNING"},
		}},
		{"stats", mapperBuilderContract{
			statement: environmentWorkMapperStatsStatement,
			bound:     buildEnvironmentWorkMapperStats(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID),
			wantID:    "EnvironmentWorkMapper.Stats", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "environmentExternalID", "workspaceUUID", "environmentExternalID"},
			wantSQLFragments:  []string{"COUNT(DISTINCT worker_id)", "INTERVAL '30 seconds'"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}

	t.Run("claim next includes session work", func(t *testing.T) {
		bound := buildEnvironmentWorkMapperClaimNext(yourbatis.DialectPostgres, &workerID, now, true)
		if strings.Contains(bound.SQL, "data->>'type'") {
			t.Fatalf("SQL unexpectedly filters session work: %q", bound.SQL)
		}
	})
}

func TestEnvironmentSandboxMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	params := environmentSandboxMapperTestWriteParams(now)
	stateParams := environmentSandboxStateParams{
		WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, State: "running",
		ProviderSandboxID: params.ProviderSandboxID, LastError: params.LastError, StoppedAt: &now,
	}
	recoveryParams := environmentSandboxRecoveryParams{
		CodeSessionExternalID: "codesession_test",
		ProviderSandboxID:     "sandbox_test",
		LastError:             "sandbox not found",
	}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: environmentSandboxMapperInsertStatement, bound: buildEnvironmentSandboxMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "EnvironmentSandboxMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID",
				"params.EnvironmentUUID", "params.EnvironmentExternalID", "params.WorkUUID", "params.WorkExternalID",
				"params.Provider", "params.Template", "params.ProviderSandboxID", "params.State", "params.Metadata",
				"params.LastError", "params.CreatedAt", "params.CreatedAt",
			},
			wantSensitiveArgumentNames: []string{"params.Metadata", "params.LastError"},
			wantSQLFragments:           []string{"INSERT INTO environment_sandboxes", "CAST($13 AS jsonb)", "RETURNING"},
		}},
		{"update", mapperBuilderContract{
			statement: environmentSandboxMapperUpdateStateStatement,
			bound:     buildEnvironmentSandboxMapperUpdateState(yourbatis.DialectPostgres, stateParams),
			wantID:    "EnvironmentSandboxMapper.UpdateState", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.State", "params.ProviderSandboxID", "params.LastError", "params.StoppedAt",
				"params.WorkspaceUUID", "params.ExternalID",
			},
			wantSensitiveArgumentNames: []string{"params.LastError"}, wantSQLFragments: []string{"UPDATE environment_sandboxes", "workspace_uuid = $5"},
		}},
		{"find active", mapperBuilderContract{
			statement: environmentSandboxMapperFindActiveForWorkStatement,
			bound:     buildEnvironmentSandboxMapperFindActiveForWork(yourbatis.DialectPostgres, params.WorkspaceUUID, params.EnvironmentExternalID, "work_test"),
			wantID:    "EnvironmentSandboxMapper.FindActiveForWork", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "environmentExternalID", "workExternalID"},
			wantSQLFragments:  []string{"FROM environment_sandboxes", "provider_sandbox_id IS NOT NULL", "LIMIT 1"},
		}},
		{"find active for code session worker statuses", mapperBuilderContract{
			statement: environmentSandboxMapperFindActiveByCodeSessionExternalIDAndWorkerStatusesStatement,
			bound: buildEnvironmentSandboxMapperFindActiveByCodeSessionExternalIDAndWorkerStatuses(
				yourbatis.DialectPostgres,
				"codesession_test",
				[]string{"idle", "running", "requires_action"},
			),
			wantID: "EnvironmentSandboxMapper.FindActiveByCodeSessionExternalIDAndWorkerStatuses", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"codeSessionExternalID", "workerStatus", "workerStatus", "workerStatus"},
			wantSQLFragments: []string{
				"JOIN environment_work", "JOIN environment_sandboxes", "worker_status IN ( $2 , $3 , $4 )",
			},
		}},
		{"schedule recovery for code session", mapperBuilderContract{
			statement: environmentSandboxMapperScheduleRecoveryForCodeSessionStatement,
			bound: buildEnvironmentSandboxMapperScheduleRecoveryForCodeSession(
				yourbatis.DialectPostgres,
				recoveryParams,
			),
			wantID:   "EnvironmentSandboxMapper.ScheduleRecoveryForCodeSession",
			wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{
				"params.ProviderSandboxID", "params.CodeSessionExternalID", "params.LastError",
			},
			wantSensitiveArgumentNames: []string{"params.LastError"},
			wantSQLFragments: []string{
				"WITH recovery_target AS", "FOR UPDATE OF code_session, work, sandbox",
				"state = 'failed'", "state = 'queued'",
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}

func TestEnvironmentMapperResultSemantics(t *testing.T) {
	ctx := context.Background()
	t.Run("environment scans string UUIDs", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: environmentMapperTestColumns(), rows: [][]driver.Value{environmentMapperTestRow()},
		})
		row, err := NewEnvironmentMapper(executor).FindByExternalID(ctx, "workspace", "environment")
		if err != nil || row.environment().UUID != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("FindByExternalID() = (%+v, %v)", row, err)
		}
	})

	t.Run("environment work scans string UUIDs", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: environmentWorkMapperTestColumns(), rows: [][]driver.Value{environmentWorkMapperTestRow()},
		})
		row, err := NewEnvironmentWorkMapper(executor).FindByExternalID(ctx, "workspace", "environment", "work")
		if err != nil || row.work().EnvironmentUUID != "00000000-0000-4000-8000-000000000003" {
			t.Fatalf("FindByExternalID() = (%+v, %v)", row, err)
		}
	})

	t.Run("zero rows preserves sql ErrNoRows", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{columns: environmentMapperTestColumns()})
		_, err := NewEnvironmentMapper(executor).FindByExternalID(ctx, "workspace", "missing")
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("FindByExternalID() error = %v, want sql.ErrNoRows", err)
		}
	})

	t.Run("scalar and rows affected", func(t *testing.T) {
		scalarExecutor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"uuid"}, rows: [][]driver.Value{{"00000000-0000-4000-8000-000000000001"}},
		})
		uuid, err := NewEnvironmentMapper(scalarExecutor).LockUUIDByExternalID(ctx, "workspace", "environment")
		if err != nil || uuid == "" {
			t.Fatalf("LockUUIDByExternalID() = (%q, %v)", uuid, err)
		}
		rowsExecutor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: 1})
		rows, err := NewEnvironmentMapper(rowsExecutor).SoftDeleteByUUID(ctx, "workspace", uuid)
		if err != nil || rows != 1 {
			t.Fatalf("SoftDeleteByUUID() = (%d, %v)", rows, err)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		row := environmentMapperTestRow()
		row[12] = "not-a-time"
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: environmentMapperTestColumns(), rows: [][]driver.Value{row},
		})
		if _, err := NewEnvironmentMapper(executor).FindByExternalID(ctx, "workspace", "environment"); err == nil {
			t.Fatal("FindByExternalID() scan error = nil")
		}
	})
}

func TestDeploymentAndEnvironmentMapperParamsPreserveJSONBoundaries(t *testing.T) {
	deployment := deploymentWriteParamsFrom(Deployment{
		AgentSnapshot: json.RawMessage(`null`),
		Metadata:      json.RawMessage(`{"key":"value"}`),
	})
	if deployment.AgentSnapshot != nil || string(deployment.Metadata) != `{"key":"value"}` {
		t.Fatalf("deployment JSON params = (%q, %q)", deployment.AgentSnapshot, deployment.Metadata)
	}

	environment := environmentWriteParamsFrom(Environment{
		Config:   json.RawMessage(`null`),
		Metadata: json.RawMessage(`{"key":"value"}`),
	})
	if environment.Config != nil || string(environment.Metadata) != `{"key":"value"}` {
		t.Fatalf("environment JSON params = (%q, %q)", environment.Config, environment.Metadata)
	}
}

func TestEnvironmentMapperPropagatesExecutionErrors(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		contract mapperExecutionErrorContract
	}{
		{"select", mapperExecutionErrorContract{
			statementID: "EnvironmentWorkMapper.FindByExternalID", kind: yourbatis.StatementSelect, query: true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewEnvironmentWorkMapper(executor).FindByExternalID(ctx, "workspace", "environment", "work")
				return err
			},
		}},
		{"returning", mapperExecutionErrorContract{
			statementID: "EnvironmentMapper.Insert", kind: yourbatis.StatementInsert, query: true,
			call: func(executor yourbatis.Executor) error {
				_, err := NewEnvironmentMapper(executor).Insert(ctx, environmentWriteParams{})
				return err
			},
		}},
		{"exec", mapperExecutionErrorContract{
			statementID: "EnvironmentKeyMapper.Upsert", kind: yourbatis.StatementInsert,
			call: func(executor yourbatis.Executor) error {
				return NewEnvironmentKeyMapper(executor).Upsert(ctx, environmentKeyUpsertParams{})
			},
		}},
		{"rows", mapperExecutionErrorContract{
			statementID: "EnvironmentMapper.SoftDeleteByUUID", kind: yourbatis.StatementUpdate,
			call: func(executor yourbatis.Executor) error {
				_, err := NewEnvironmentMapper(executor).SoftDeleteByUUID(ctx, "workspace", "environment")
				return err
			},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperExecutionError(t, test.contract) })
	}
}

func environmentMapperTestWriteParams(now time.Time) environmentWriteParams {
	return environmentWriteParams{
		UUID: "00000000-0000-4000-8000-000000000001", ExternalID: "env_test",
		OrganizationUUID:    "00000000-0000-4000-8000-000000000002",
		WorkspaceUUID:       "00000000-0000-4000-8000-000000000003",
		CreatedByAPIKeyUUID: "00000000-0000-4000-8000-000000000004",
		Name:                "test", Description: "description", Config: []byte(`{}`), Metadata: []byte(`{}`),
		Provider: "local", ResolvedTemplate: "default", CreatedAt: now, UpdatedAt: now,
	}
}

func environmentWorkMapperTestWriteParams(now time.Time) environmentWorkWriteParams {
	secret := "secret"
	return environmentWorkWriteParams{
		UUID: "00000000-0000-4000-8000-000000000004", ExternalID: "work_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001",
		WorkspaceUUID:    "00000000-0000-4000-8000-000000000002",
		EnvironmentUUID:  "00000000-0000-4000-8000-000000000003", EnvironmentExternalID: "env_test",
		Data: []byte(`{"type":"session"}`), Metadata: []byte(`{}`), Secret: &secret, State: "queued", CreatedAt: now,
	}
}

func environmentSandboxMapperTestWriteParams(now time.Time) environmentSandboxWriteParams {
	workUUID := "00000000-0000-4000-8000-000000000004"
	workExternalID := "work_test"
	providerSandboxID := "sandbox_test"
	lastError := "test"
	return environmentSandboxWriteParams{
		UUID: "00000000-0000-4000-8000-000000000005", ExternalID: "envsandbox_test",
		OrganizationUUID: "00000000-0000-4000-8000-000000000001",
		WorkspaceUUID:    "00000000-0000-4000-8000-000000000002",
		EnvironmentUUID:  "00000000-0000-4000-8000-000000000003", EnvironmentExternalID: "env_test",
		WorkUUID: &workUUID, WorkExternalID: &workExternalID, Provider: "local", Template: "default",
		ProviderSandboxID: &providerSandboxID, State: "running", Metadata: []byte(`{}`), LastError: &lastError, CreatedAt: now,
	}
}

func environmentMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "created_by_api_key_uuid",
		"name", "description", "config", "metadata", "scope", "provider", "resolved_template",
		"created_at", "updated_at", "archived_at", "deleted_at",
	}
}

func environmentMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000001", "env_test", "00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000004",
		"test", "description", []byte(`{}`), []byte(`{}`), nil, "local", "default", now, now, nil, nil,
	}
}

func environmentWorkMapperTestColumns() []string {
	return []string{
		"uuid", "external_id", "organization_uuid", "workspace_uuid", "environment_uuid",
		"environment_external_id", "data", "metadata", "secret", "state", "claimed_by_worker_id",
		"claim_expires_at", "acknowledged_at", "started_at", "latest_heartbeat_at", "heartbeat_ttl_seconds",
		"stop_requested_at", "stopped_at", "created_at", "updated_at", "deleted_at",
	}
}

func environmentWorkMapperTestRow() []driver.Value {
	now := time.Date(2026, time.August, 5, 1, 2, 3, 0, time.UTC)
	return []driver.Value{
		"00000000-0000-4000-8000-000000000004", "work_test", "00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003", "env_test",
		[]byte(`{"type":"session"}`), []byte(`{}`), "secret", "queued", nil, nil, nil, nil, nil, nil, nil, nil, now, now, nil,
	}
}
