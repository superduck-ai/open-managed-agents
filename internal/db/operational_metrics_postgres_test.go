//go:build integration

package db

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
)

func TestOperationalStateCountsPostgres(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, standard, migrations := newIsolatedMigrationTestDatabase(t, cfg.Database.URL)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var heartbeatIndexes, liveStateIndexes int
	if err := standard.QueryRowContext(ctx, `
		select count(*) filter (where indexdef like '%updated_at%'), count(*) filter (where indexname = 'environment_work_live_state_v2_idx')
		from pg_indexes where schemaname = current_schema() and tablename = 'environment_work'
	`).Scan(&heartbeatIndexes, &liveStateIndexes); err != nil {
		t.Fatal(err)
	}
	if heartbeatIndexes != 0 || liveStateIndexes != 1 {
		t.Fatalf("environment_work indexes: updated_at=%d live_state=%d", heartbeatIndexes, liveStateIndexes)
	}
	database := &DB{mapperDB: yourbatis.NewDB(standard, yourbatis.DialectPostgres)}
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	cutoff := now.Add(-30 * time.Minute)
	empty, err := database.OperationalStateCounts(ctx, cutoff, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	for _, counts := range []map[string]int64{empty.Sessions, empty.EnvironmentWork, empty.EnvironmentSandboxes} {
		for _, value := range counts {
			total += value
		}
	}
	for _, value := range empty.CodeSessions {
		total += value
	}
	if !reflect.DeepEqual(empty.BrainClaims, map[string]int64{"waiting": 0, "claimed": 0, "takeover": 0}) || empty.OldestWaitingBrainClaimSeconds != 0 {
		t.Fatalf("empty brain claim counts: %+v", empty)
	}
	if total != 0 || len(empty.Sessions) != 2 || len(empty.CodeSessions) != 8 || empty.StaleRunningSessions+empty.StuckStartingWork+empty.OldestQueuedWorkSeconds != 0 {
		t.Fatalf("empty database counts: %+v", empty)
	}
	if err := database.Seed(ctx, config.BootstrapConfig{SeedAPIKeys: []config.SeedAPIKey{{ExternalID: "apikey_ops_metrics", Key: "state-metrics-test-key"}}}); err != nil {
		t.Fatal(err)
	}
	key, err := database.GetAPIKey(ctx, auth.HashAPIKey("state-metrics-test-key"))
	if err != nil {
		t.Fatal(err)
	}
	organization, workspace := key.OrganizationUUID.String(), key.WorkspaceUUID.String()
	environment, err := database.CreateEnvironment(ctx, Environment{UUID: uuid.NewV4().String(), ExternalID: "env_metrics", OrganizationUUID: organization, WorkspaceUUID: workspace, Name: "metrics", Config: json.RawMessage(`{}`), Metadata: json.RawMessage(`{}`), Provider: "e2b", CreatedAt: old})
	if err != nil {
		t.Fatal(err)
	}
	sessionMapper := NewSessionMapper(database.mapperDB)
	var owner, runningOwner Session
	for i, status := range []string{"running", "rescheduling", "idle", "idle", "idle", "running"} {
		session := Session{UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("ses_metrics_%d", i), OrganizationUUID: organization, WorkspaceUUID: workspace, CreatedByAPIKeyUUID: key.UUID.String(), EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, AgentUUID: uuid.NewV4().String(), AgentExternalID: "agent_metrics", AgentVersion: 1, AgentSnapshot: json.RawMessage(`{}`), Metadata: json.RawMessage(`{}`), Status: status, Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), OutcomeEvaluations: json.RawMessage(`[]`), CreatedAt: old}
		if _, err := sessionMapper.Insert(ctx, sessionWriteParameters(session)); err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0:
			runningOwner = session
		case 2:
			owner = session
		case 3:
			if _, err := sessionMapper.Archive(ctx, workspace, session.ExternalID); err != nil {
				t.Fatal(err)
			}
			if _, err := sessionMapper.SetStatus(ctx, workspace, session.ExternalID, "running"); err != nil {
				t.Fatal(err)
			}
		case 4:
			if _, err := sessionMapper.SoftDelete(ctx, workspace, session.ExternalID); err != nil {
				t.Fatal(err)
			}
		case 5:
			if _, err := standard.ExecContext(ctx, `
				insert into session_events (external_id, session_external_id, event_type, payload, organization_uuid, workspace_uuid, session_uuid, created_at)
				values ('sevt_metrics_progress', $1, 'agent.message', '{}', $2, $3, $4, $5)
			`, session.ExternalID, organization, workspace, session.UUID, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i, state := range []string{"queued", "starting", "active", "stopping", "stopped"} {
		_, err := database.CreateEnvironmentWork(ctx, EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("work_metrics_%d", i), OrganizationUUID: organization, WorkspaceUUID: workspace, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, SessionUUID: owner.UUID, Metadata: json.RawMessage(`{}`), State: state, CreatedAt: old})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.CreateEnvironmentWork(ctx, EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: "work_metrics_backoff", OrganizationUUID: organization, WorkspaceUUID: workspace, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, SessionUUID: owner.UUID, Metadata: json.RawMessage(`{}`), State: "queued", CreatedAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for i, state := range []string{"creating", "running", "stopping", "failed", "stopped"} {
		if _, err := database.CreateEnvironmentSandbox(ctx, EnvironmentSandbox{UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("envsbx_metrics_%d", i), OrganizationUUID: organization, WorkspaceUUID: workspace, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, Provider: "e2b", Template: "test", State: state, Metadata: json.RawMessage(`{}`), CreatedAt: old}); err != nil {
			t.Fatal(err)
		}
	}
	codeMapper := NewCodeSessionMapper(database.mapperDB)
	for i, status := range []string{"active", "active", "initializing", "terminated"} {
		code, err := codeMapper.Insert(ctx, createCodeSessionParams{ExternalID: fmt.Sprintf("cse_metrics_%d", i), OrganizationUUID: organization, WorkspaceUUID: workspace, SessionUUID: owner.UUID, SessionExternalID: owner.ExternalID, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, WorkDir: "/workspace", PermissionMode: "default", Model: "model", Status: status, Metadata: []byte(`{}`), CreatedAt: old})
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if _, err := codeMapper.UpdateWorkerState(ctx, updateCodeSessionWorkerStateParams{UUID: code.UUID, WorkerStatus: "running", RequiresActionDetails: []byte(`null`), ExternalMetadata: []byte(`{}`), Now: now}); err != nil {
				t.Fatal(err)
			}
			if _, err := codeMapper.UpdateConnection(ctx, updateCodeSessionConnectionParams{ExternalID: code.ExternalID, Status: "connected", Connected: true, Now: now}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := standard.ExecContext(ctx, `
		update code_sessions
		set metadata = '{"config":{"worker_transport":"claim"}}',
			worker_claim_requested_at = now() - interval '90 seconds',
			worker_claim_expires_at = case external_id when 'cse_metrics_1' then now() + interval '30 seconds' end
		where external_id in ('cse_metrics_0', 'cse_metrics_1', 'cse_metrics_3')
	`); err != nil {
		t.Fatal(err)
	}
	takeover, err := codeMapper.Insert(ctx, createCodeSessionParams{ExternalID: "cse_metrics_takeover", OrganizationUUID: organization, WorkspaceUUID: workspace, SessionUUID: runningOwner.UUID, SessionExternalID: runningOwner.ExternalID, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, WorkDir: "/workspace", PermissionMode: "default", Model: "model", Status: "active", Metadata: []byte(`{"config":{"worker_transport":"claim"}}`), CreatedAt: old})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := standard.ExecContext(ctx, `update code_sessions set worker_lease_expires_at = now() - interval '1 minute' where uuid = $1`, takeover.UUID); err != nil {
		t.Fatal(err)
	}
	counts, err := database.OperationalStateCounts(ctx, cutoff, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(counts.BrainClaims, map[string]int64{"waiting": 1, "claimed": 1, "takeover": 1}) || counts.OldestWaitingBrainClaimSeconds < 89 || counts.OldestWaitingBrainClaimSeconds > 95 {
		t.Fatalf("brain claim counts: %+v oldest=%d", counts.BrainClaims, counts.OldestWaitingBrainClaimSeconds)
	}
	if !reflect.DeepEqual(counts.Sessions, map[string]int64{"running": 2, "rescheduling": 1}) || counts.StaleRunningSessions != 1 {
		t.Fatalf("session counts: %+v", counts)
	}
	if !reflect.DeepEqual(counts.EnvironmentWork, map[string]int64{"queued": 2, "starting": 1, "active": 1, "stopping": 1}) || counts.StuckStartingWork != 1 || counts.OldestQueuedWorkSeconds < 3599 || counts.OldestQueuedWorkSeconds > 3605 {
		t.Fatalf("work counts: %+v", counts)
	}
	if !reflect.DeepEqual(counts.EnvironmentSandboxes, map[string]int64{"creating": 1, "running": 1, "stopping": 1}) {
		t.Fatalf("sandbox counts: %+v", counts)
	}
	codeCounts := map[string]int64{}
	for state, total := range counts.CodeSessions {
		if total > 0 {
			codeCounts[state.WorkerStatus+"/"+state.ConnectionStatus] = total
		}
	}
	if !reflect.DeepEqual(codeCounts, map[string]int64{"idle/disconnected": 2, "running/connected": 1}) {
		t.Fatalf("code session counts: %v", codeCounts)
	}
	fresh, err := database.OperationalStateCounts(ctx, old.Add(-time.Minute), old.Add(-time.Minute))
	if err != nil || fresh.StaleRunningSessions != 0 || fresh.StuckStartingWork != 0 {
		t.Fatalf("fresh thresholds: %+v, %v", fresh, err)
	}
}
