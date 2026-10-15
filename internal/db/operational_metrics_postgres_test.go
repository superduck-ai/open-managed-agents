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
	database := &DB{mapperDB: yourbatis.NewDB(standard, yourbatis.DialectPostgres)}
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	cutoff := now.Add(-30 * time.Minute)
	empty, err := database.OperationalStateCounts(ctx, cutoff, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Sessions)+len(empty.CodeSessions)+len(empty.EnvironmentWork)+len(empty.EnvironmentSandboxes) != 0 || empty.StaleRunningSessions+empty.StuckStartingWork+empty.OldestQueuedWorkSeconds != 0 {
		t.Fatalf("empty database counts: %+v", empty)
	}
	if err := database.Seed(ctx, []config.SeedAPIKey{{ExternalID: "apikey_ops_metrics", Key: "state-metrics-test-key"}}); err != nil {
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
	var owner Session
	for i, status := range []string{"running", "rescheduling", "idle", "idle", "idle"} {
		session := Session{UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("ses_metrics_%d", i), OrganizationUUID: organization, WorkspaceUUID: workspace, CreatedByAPIKeyUUID: key.UUID.String(), EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, AgentUUID: uuid.NewV4().String(), AgentExternalID: "agent_metrics", AgentVersion: 1, AgentSnapshot: json.RawMessage(`{}`), Metadata: json.RawMessage(`{}`), Status: status, Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), OutcomeEvaluations: json.RawMessage(`[]`), CreatedAt: old}
		if _, err := sessionMapper.Insert(ctx, sessionWriteParameters(session)); err != nil {
			t.Fatal(err)
		}
		switch i {
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
		}
	}
	var workRows []EnvironmentWork
	for i, state := range []string{"queued", "starting", "active", "stopping", "stopped"} {
		work, err := database.CreateEnvironmentWork(ctx, EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("work_metrics_%d", i), OrganizationUUID: organization, WorkspaceUUID: workspace, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, SessionUUID: owner.UUID, Metadata: json.RawMessage(`{}`), State: state, CreatedAt: old})
		if err != nil {
			t.Fatal(err)
		}
		workRows = append(workRows, work)
	}
	if _, err := database.CreateEnvironmentWork(ctx, EnvironmentWork{UUID: uuid.NewV4().String(), ExternalID: "work_metrics_backoff", OrganizationUUID: organization, WorkspaceUUID: workspace, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, SessionUUID: owner.UUID, Metadata: json.RawMessage(`{}`), State: "queued", CreatedAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for i, state := range []string{"creating", "running", "stopping", "failed", "stopped"} {
		work := workRows[i]
		if _, err := database.CreateEnvironmentSandbox(ctx, EnvironmentSandbox{UUID: uuid.NewV4().String(), ExternalID: fmt.Sprintf("envsbx_metrics_%d", i), OrganizationUUID: organization, WorkspaceUUID: workspace, EnvironmentUUID: environment.UUID, EnvironmentExternalID: environment.ExternalID, WorkUUID: &work.UUID, WorkExternalID: &work.ExternalID, Provider: "e2b", Template: "test", State: state, Metadata: json.RawMessage(`{}`), CreatedAt: old}); err != nil {
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
	counts, err := database.OperationalStateCounts(ctx, cutoff, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	stateMap := func(rows []StateCount) map[string]int64 {
		result := map[string]int64{}
		for _, row := range rows {
			result[row.State] = row.Total
		}
		return result
	}
	if !reflect.DeepEqual(stateMap(counts.Sessions), map[string]int64{"running": 1, "rescheduling": 1}) || counts.StaleRunningSessions != 1 {
		t.Fatalf("session counts: %+v", counts)
	}
	if !reflect.DeepEqual(stateMap(counts.EnvironmentWork), map[string]int64{"queued": 2, "starting": 1, "active": 1, "stopping": 1}) || counts.StuckStartingWork != 1 || counts.OldestQueuedWorkSeconds < 3599 || counts.OldestQueuedWorkSeconds > 3605 {
		t.Fatalf("work counts: %+v", counts)
	}
	if !reflect.DeepEqual(stateMap(counts.EnvironmentSandboxes), map[string]int64{"creating": 1, "running": 1, "stopping": 1}) {
		t.Fatalf("sandbox counts: %+v", counts)
	}
	codeCounts := map[string]int64{}
	for _, row := range counts.CodeSessions {
		codeCounts[row.WorkerStatus+"/"+row.ConnectionStatus] = row.Total
	}
	if !reflect.DeepEqual(codeCounts, map[string]int64{"idle/disconnected": 1, "running/connected": 1}) {
		t.Fatalf("code session counts: %v", codeCounts)
	}
	fresh, err := database.OperationalStateCounts(ctx, old.Add(-time.Minute), old.Add(-time.Minute))
	if err != nil || fresh.StaleRunningSessions != 0 || fresh.StuckStartingWork != 0 {
		t.Fatalf("fresh thresholds: %+v, %v", fresh, err)
	}
}
