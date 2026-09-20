package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestDreamMapperBuilderContracts(t *testing.T) {
	now := time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)
	params := insertDreamParams{
		UUID: "00000000-0000-4000-8000-000000000001", ExternalID: "drm_0123456789abcdefghijklmn",
		OrganizationUUID: "00000000-0000-4000-8000-000000000002", WorkspaceUUID: "00000000-0000-4000-8000-000000000003",
		Status: "pending", Model: "claude-sonnet-4-6", Inputs: []byte(`[{"type":"memory_store"}]`), CreatedAt: now,
	}
	page := listDreamsParams{WorkspaceUUID: params.WorkspaceUUID, Limit: 21, HasCursor: true, CursorCreatedAt: now, CursorUUID: params.UUID}
	resources := recordDreamResourcesParams{WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, WorkerID: "dream-worker", OutputMemoryStoreUUID: "00000000-0000-4000-8000-000000000004", OutputMemoryStoreID: "memstore_output", InternalSessionUUID: "00000000-0000-4000-8000-000000000005", InternalSessionID: "sess_internal", Now: now}
	running := markDreamRunningParams{WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, WorkerID: "dream-worker", OutputMemoryStoreID: "memstore_output", InternalSessionID: "sess_internal", Now: now}
	claim := claimPendingDreamParams{WorkerID: "dream-worker", ClaimExpiresAt: now.Add(time.Minute)}
	renew := renewPendingDreamClaimParams{WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, WorkerID: "dream-worker", ClaimExpiresAt: now.Add(time.Minute), Now: now}
	retry := schedulePendingDreamRetryParams{WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, WorkerID: "dream-worker", NextAttemptAt: now.Add(time.Minute), LastError: "storage unavailable", Now: now}
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"insert", mapperBuilderContract{
			statement: dreamMapperInsertStatement, bound: buildDreamMapperInsert(yourbatis.DialectPostgres, params),
			wantID: "DreamMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames:          []string{"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.CreatedByAPIKeyUUID", "params.RuntimeUserUUID", "params.Status", "params.Model", "params.Instructions", "params.Inputs", "params.CreatedAt", "params.CreatedAt"},
			wantSensitiveArgumentNames: []string{"params.Inputs"}, wantSQLFragments: []string{"INSERT INTO dreams", "CAST($10 AS jsonb)", "RETURNING"},
		}},
		{"list page", mapperBuilderContract{
			statement: dreamMapperListPageStatement, bound: buildDreamMapperListPage(yourbatis.DialectPostgres, page),
			wantID: "DreamMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.CursorCreatedAt", "params.CursorCreatedAt", "params.CursorUUID", "params.Limit"},
			wantSQLFragments:  []string{"archived_at IS NULL", "ORDER BY created_at DESC, uuid DESC", "LIMIT $5"},
		}},
		{"record pending resources", mapperBuilderContract{
			statement: dreamMapperRecordPendingResourcesStatement, bound: buildDreamMapperRecordPendingResources(yourbatis.DialectPostgres, resources),
			wantID: "DreamMapper.RecordPendingResources", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.OutputMemoryStoreUUID", "params.InternalSessionUUID", "params.OutputMemoryStoreID", "params.InternalSessionID", "params.Now", "params.WorkspaceUUID", "params.ExternalID", "params.WorkerID"},
			wantSQLFragments:  []string{"outputs = jsonb_build_array", "status = 'pending'", "RETURNING"},
		}},
		{"claim pending", mapperBuilderContract{
			statement: dreamMapperClaimNextPendingStatement, bound: buildDreamMapperClaimNextPending(yourbatis.DialectPostgres, claim),
			wantID: "DreamMapper.ClaimNextPending", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkerID", "params.ClaimExpiresAt"},
			wantSQLFragments:  []string{"FOR UPDATE SKIP LOCKED", "attempt_count = attempt_count + 1", "claim_expires_at", "RETURNING"},
		}},
		{"list stopped with active session", mapperBuilderContract{
			statement: dreamMapperListStoppedWithActiveSessionStatement, bound: buildDreamMapperListStoppedWithActiveSession(yourbatis.DialectPostgres, 20),
			wantID: "DreamMapper.ListStoppedWithActiveSession", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"limit"},
			wantSQLFragments:  []string{"status IN ('canceled', 'failed')", "sessions.status IN ('running', 'rescheduling')", "ORDER BY ended_at ASC", "LIMIT $1"},
		}},
		{"list terminal awaiting runtime reclaim", mapperBuilderContract{
			statement: dreamMapperListTerminalAwaitingRuntimeReclaimStatement, bound: buildDreamMapperListTerminalAwaitingRuntimeReclaim(yourbatis.DialectPostgres, 20),
			wantID: "DreamMapper.ListTerminalAwaitingRuntimeReclaim", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"limit"},
			wantSQLFragments: []string{
				"status IN ('completed', 'failed', 'canceled')",
				"environment_work.state <> 'stopped'",
				"sessions.status IN ('running', 'rescheduling')",
				"ORDER BY ended_at ASC, uuid ASC",
				"LIMIT $1",
			},
		}},
		{"renew pending claim", mapperBuilderContract{
			statement: dreamMapperRenewPendingClaimStatement, bound: buildDreamMapperRenewPendingClaim(yourbatis.DialectPostgres, renew),
			wantID: "DreamMapper.RenewPendingClaim", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.ClaimExpiresAt", "params.Now", "params.WorkspaceUUID", "params.ExternalID", "params.WorkerID"},
			wantSQLFragments:  []string{"claim_expires_at", "status = 'pending'", "claimed_by_worker_id", "archived_at IS NULL"},
		}},
		{"schedule pending retry", mapperBuilderContract{
			statement: dreamMapperSchedulePendingRetryStatement, bound: buildDreamMapperSchedulePendingRetry(yourbatis.DialectPostgres, retry),
			wantID: "DreamMapper.SchedulePendingRetry", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.NextAttemptAt", "params.LastError", "params.Now", "params.WorkspaceUUID", "params.ExternalID", "params.WorkerID"},
			wantSensitiveArgumentNames: []string{"params.LastError"},
			wantSQLFragments:           []string{"next_attempt_at", "claimed_by_worker_id = NULL", "status = 'pending'", "RETURNING"},
		}},
		{"mark running", mapperBuilderContract{
			statement: dreamMapperMarkRunningStatement, bound: buildDreamMapperMarkRunning(yourbatis.DialectPostgres, running),
			wantID: "DreamMapper.MarkRunning", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.Now", "params.Now", "params.WorkspaceUUID", "params.ExternalID", "params.WorkerID", "params.OutputMemoryStoreID", "params.InternalSessionID"},
			wantSQLFragments:  []string{"status = 'running'", "started_at = COALESCE", "status = 'pending'", "RETURNING"},
		}},
		{"update running usage", mapperBuilderContract{
			statement: dreamMapperUpdateRunningUsageStatement, bound: buildDreamMapperUpdateRunningUsage(yourbatis.DialectPostgres, updateRunningDreamUsageParams{WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, Usage: []byte(`{"input_tokens":1}`), Now: now}),
			wantID: "DreamMapper.UpdateRunningUsage", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.Usage", "params.Now", "params.WorkspaceUUID", "params.ExternalID"},
			wantSensitiveArgumentNames: []string{"params.Usage"},
			wantSQLFragments:           []string{"usage = CAST($1 AS jsonb)", "status = 'running'", "RETURNING"},
		}},
		{"mark terminal", mapperBuilderContract{
			statement: dreamMapperMarkTerminalStatement, bound: buildDreamMapperMarkTerminal(yourbatis.DialectPostgres, markDreamTerminalParams{WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, Status: "canceled", Error: []byte("null"), Usage: []byte("{}"), Now: now}),
			wantID: "DreamMapper.MarkTerminal", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.Status", "params.Error", "params.Usage", "params.Now", "params.Now", "params.WorkspaceUUID", "params.ExternalID"},
			wantSensitiveArgumentNames: []string{"params.Error", "params.Usage"},
			wantSQLFragments:           []string{"ended_at = $4", "status IN ('pending', 'running')", "RETURNING"},
		}},
		{"mark claimed failed", mapperBuilderContract{
			statement: dreamMapperMarkClaimedFailedStatement, bound: buildDreamMapperMarkClaimedFailed(yourbatis.DialectPostgres, markClaimedDreamFailedParams{WorkspaceUUID: params.WorkspaceUUID, ExternalID: params.ExternalID, WorkerID: "dream-worker", Error: []byte(`{"type":"internal_error"}`), Usage: []byte(`{}`), Now: now}),
			wantID: "DreamMapper.MarkClaimedFailed", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.Error", "params.Usage", "params.Now", "params.Now", "params.WorkspaceUUID", "params.ExternalID", "params.WorkerID"},
			wantSensitiveArgumentNames: []string{"params.Error", "params.Usage"},
			wantSQLFragments:           []string{"status = 'failed'", "execution_state = 'terminal'", "claimed_by_worker_id", "RETURNING"},
		}},
		{"list awaiting internal session archive", mapperBuilderContract{
			statement: dreamMapperListAwaitingInternalSessionArchiveStatement, bound: buildDreamMapperListAwaitingInternalSessionArchive(yourbatis.DialectPostgres, 20),
			wantID: "DreamMapper.ListAwaitingInternalSessionArchive", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"limit"},
			wantSQLFragments:  []string{"status IN ('completed', 'failed', 'canceled')", "sessions.archived_at IS NULL", "sessions.status NOT IN ('running', 'rescheduling')", "environment_work.state <> 'stopped'", "ORDER BY ended_at ASC, uuid ASC", "LIMIT $1"},
		}},
		{"archive", mapperBuilderContract{
			statement: dreamMapperArchiveByExternalIDStatement, bound: buildDreamMapperArchiveByExternalID(yourbatis.DialectPostgres, params.WorkspaceUUID, params.ExternalID),
			wantID: "DreamMapper.ArchiveByExternalID", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"workspaceUUID", "externalID"}, wantSQLFragments: []string{"archived_at = COALESCE", "updated_at = NOW()", "RETURNING"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}
