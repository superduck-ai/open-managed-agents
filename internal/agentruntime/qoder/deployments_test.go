package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
)

func TestQoderDeploymentLifecycleRunAndIDOR(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	var upstreamHits atomic.Int32
	var paused atomic.Bool
	var archived atomic.Bool
	var lastCreateBody map[string]any
	var lastUpdateBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/deployments":
			_ = json.NewDecoder(r.Body).Decode(&lastCreateBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "depl_remote_1",
				"type":           "deployment",
				"agent":          map[string]any{"id": "agent_remote_1", "type": "agent", "version": 2},
				"environment_id": "env_remote_1",
				"name":           lastCreateBody["name"],
				"description":    lastCreateBody["description"],
				"metadata":       lastCreateBody["metadata"],
				"initial_events": lastCreateBody["initial_events"],
				"resources": []map[string]any{
					{"id": "sesr_remote_1", "type": "memory_store", "memory_store_id": "memstore_remote_1", "access": "read_write"},
				},
				"vault_ids":  []string{},
				"budget":     lastCreateBody["budget"],
				"schedule":   lastCreateBody["schedule"],
				"status":     "active",
				"created_at": now,
				"updated_at": now,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/deployments/depl_remote_1":
			status := "active"
			if paused.Load() {
				status = "paused"
			}
			var archivedAt any
			if archived.Load() {
				archivedAt = now.Add(time.Minute)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "depl_remote_1",
				"type":           "deployment",
				"agent":          map[string]any{"id": "agent_remote_1", "type": "agent", "version": 2},
				"environment_id": "env_remote_1",
				"name":           "Daily Digest",
				"description":    "Updated desc",
				"metadata":       map[string]string{watermarkOrgKey: "org-a", watermarkWorkspaceKey: "ws-a", "team": "ops"},
				"initial_events": []map[string]any{{"type": "user.message", "content": []map[string]any{{"type": "text", "text": "hi"}}}},
				"resources": []map[string]any{
					{"id": "sesr_remote_1", "type": "memory_store", "memory_store_id": "memstore_remote_1", "access": "read_write"},
				},
				"vault_ids":   []string{},
				"budget":      map[string]any{"type": "limit", "max_credit_cost": "50.00"},
				"status":      status,
				"archived_at": archivedAt,
				"created_at":  now,
				"updated_at":  now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/deployments/depl_remote_1":
			_ = json.NewDecoder(r.Body).Decode(&lastUpdateBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "depl_remote_1",
				"type":           "deployment",
				"agent":          map[string]any{"id": "agent_remote_1", "type": "agent", "version": 2},
				"environment_id": "env_remote_1",
				"name":           lastUpdateBody["name"],
				"description":    "Updated desc",
				"metadata":       lastUpdateBody["metadata"],
				"status":         "active",
				"created_at":     now,
				"updated_at":     now.Add(time.Second),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/deployments/depl_remote_1/pause":
			paused.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "depl_remote_1",
				"agent":          map[string]any{"id": "agent_remote_1", "version": 2},
				"environment_id": "env_remote_1",
				"name":           "Daily Digest",
				"status":         "paused",
				"paused_reason":  map[string]any{"type": "manual"},
				"created_at":     now,
				"updated_at":     now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/deployments/depl_remote_1/unpause":
			paused.Store(false)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "depl_remote_1",
				"agent":          map[string]any{"id": "agent_remote_1", "version": 2},
				"environment_id": "env_remote_1",
				"name":           "Daily Digest",
				"status":         "active",
				"created_at":     now,
				"updated_at":     now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/deployments/depl_remote_1/run":
			sessionID := "sesn_remote_run_1"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              "drun_remote_1",
				"deployment_id":   "depl_remote_1",
				"agent":           map[string]any{"id": "agent_remote_1", "version": 2},
				"session_id":      sessionID,
				"trigger_context": map[string]any{"type": "manual"},
				"created_at":      now.Add(2 * time.Second),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/deployment_runs":
			if r.URL.Query().Get("deployment_id") != "depl_remote_1" {
				t.Errorf("unexpected deployment_id query: %s", r.URL.Query().Get("deployment_id"))
			}
			sessionID := "sesn_remote_run_1"
			schedSessionID := "sesn_remote_sched_2"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":              "drun_remote_1",
						"deployment_id":   "depl_remote_1",
						"agent":           map[string]any{"id": "agent_remote_1", "version": 2},
						"session_id":      sessionID,
						"trigger_context": map[string]any{"type": "manual"},
						"created_at":      now.Add(2 * time.Second),
					},
					{
						"id":              "drun_remote_2",
						"deployment_id":   "depl_remote_1",
						"agent":           map[string]any{"id": "agent_remote_1", "version": 2},
						"session_id":      schedSessionID,
						"trigger_context": map[string]any{"type": "schedule", "scheduled_at": now.Add(3 * time.Second).Format(time.RFC3339)},
						"created_at":      now.Add(3 * time.Second),
					},
					{
						"id":              "drun_other_tenant",
						"deployment_id":   "depl_remote_other",
						"agent":           map[string]any{"id": "agent_other", "version": 1},
						"trigger_context": map[string]any{"type": "manual"},
						"created_at":      now.Add(4 * time.Second),
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/deployment_runs/drun_remote_1":
			sessionID := "sesn_remote_run_1"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              "drun_remote_1",
				"deployment_id":   "depl_remote_1",
				"agent":           map[string]any{"id": "agent_remote_1", "version": 2},
				"session_id":      sessionID,
				"trigger_context": map[string]any{"type": "manual"},
				"created_at":      now.Add(2 * time.Second),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/deployments/depl_remote_1/archive":
			archived.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "depl_remote_1",
				"agent":          map[string]any{"id": "agent_remote_1", "version": 2},
				"environment_id": "env_remote_1",
				"name":           "Daily Digest",
				"status":         "active",
				"archived_at":    now.Add(time.Minute),
				"created_at":     now,
				"updated_at":     now.Add(time.Minute),
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scopeA := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: rt.profile.UUID,
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	scopeB := agentruntime.Scope{
		OrganizationUUID:   "org-b",
		WorkspaceUUID:      "ws-b",
		RuntimeProfileUUID: rt.profile.UUID,
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	_, _ = rt.bindings.Bind(ctx, scopeA, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeAgent,
		OMAExternalID: "agent_oma_1",
		RemoteID:      "agent_remote_1",
	}, json.RawMessage(`{}`))
	_, _ = rt.bindings.Bind(ctx, scopeA, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeEnvironment,
		OMAExternalID: "env_oma_1",
		RemoteID:      "env_remote_1",
	}, json.RawMessage(`{}`))
	_, _ = rt.bindings.Bind(ctx, scopeA, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeMemoryStore,
		OMAExternalID: "memstore_oma_1",
		RemoteID:      "memstore_remote_1",
	}, json.RawMessage(`{}`))

	created, err := rt.CreateDeployment(ctx, scopeA, agentruntime.CreateDeploymentInput{
		AgentID:       "agent_oma_1",
		AgentVersion:  2,
		EnvironmentID: "env_oma_1",
		Name:          "Daily Digest",
		Description:   "Run every day",
		Metadata:      map[string]string{"team": "ops"},
		InitialEvents: []json.RawMessage{json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`)},
		Resources: []agentruntime.AddSessionResourceInput{
			{
				Type:          "memory_store",
				MemoryStoreID: "memstore_oma_1",
				Payload:       json.RawMessage(`{"type":"memory_store","memory_store_id":"memstore_oma_1","access":"read_write"}`),
			},
		},
		Budget: &agentruntime.SessionBudget{Mode: agentruntime.BudgetModeCredits, MaxCreditCost: "50.00"},
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if created.Agent.ID != "agent_oma_1" || created.EnvironmentID != "env_oma_1" {
		t.Fatalf("mapped ids mismatch: %+v", created)
	}
	if _, hasWatermark := created.Metadata[watermarkOrgKey]; hasWatermark {
		t.Fatalf("watermark leaked into metadata: %+v", created.Metadata)
	}
	if len(created.Resources) != 1 || !containsJSONValue(created.Resources[0], "memstore_oma_1") {
		t.Fatalf("expected memory_store_id rewritten back to oma id: %s", string(created.Resources[0]))
	}
	resList, _ := lastCreateBody["resources"].([]any)
	if len(resList) != 1 {
		t.Fatalf("expected 1 resource sent upstream: %+v", lastCreateBody["resources"])
	}
	if resList[0].(map[string]any)["memory_store_id"] != "memstore_remote_1" {
		t.Fatalf("expected remote memory_store_id sent upstream: %+v", resList[0])
	}

	hitsBeforeIDOR := upstreamHits.Load()
	if _, err := rt.GetDeployment(ctx, scopeB, created.ID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant GetDeployment err = %v, want ErrResourceNotFound", err)
	}
	if upstreamHits.Load() != hitsBeforeIDOR {
		t.Fatalf("cross-tenant GetDeployment must not hit upstream")
	}

	newName := "Daily Digest v2"
	updated, err := rt.UpdateDeployment(ctx, scopeA, created.ID, agentruntime.UpdateDeploymentInput{
		Name:        &newName,
		Metadata:    map[string]string{"team": "platform"},
		MetadataSet: true,
	})
	if err != nil || updated.Name != newName {
		t.Fatalf("UpdateDeployment = (%+v, %v)", updated, err)
	}

	pausedDep, err := rt.PauseDeployment(ctx, scopeA, created.ID)
	if err != nil || pausedDep.Status != "paused" {
		t.Fatalf("PauseDeployment = (%+v, %v)", pausedDep, err)
	}
	unpausedDep, err := rt.UnpauseDeployment(ctx, scopeA, created.ID)
	if err != nil || unpausedDep.Status != "active" {
		t.Fatalf("UnpauseDeployment = (%+v, %v)", unpausedDep, err)
	}

	run, err := rt.RunDeployment(ctx, scopeA, created.ID)
	if err != nil {
		t.Fatalf("RunDeployment: %v", err)
	}
	if run.DeploymentID != created.ID || run.Agent.ID != "agent_oma_1" || run.SessionID == nil || *run.SessionID == "" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if _, err := rt.bindings.ResolveOwned(ctx, scopeA, agentruntime.ResourceTypeSession, *run.SessionID); err != nil {
		t.Fatalf("expected run session claimed for workspace: %v", err)
	}

	fetchedRun, err := rt.GetDeploymentRun(ctx, scopeA, run.ID)
	if err != nil || fetchedRun.ID != run.ID || fetchedRun.SessionID == nil || *fetchedRun.SessionID != *run.SessionID {
		t.Fatalf("GetDeploymentRun = (%+v, %v)", fetchedRun, err)
	}
	if _, err := rt.GetDeploymentRun(ctx, scopeB, run.ID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant GetDeploymentRun err = %v, want ErrResourceNotFound", err)
	}

	runsPage, err := rt.ListDeploymentRuns(ctx, scopeA, agentruntime.ListDeploymentRunsQuery{})
	if err != nil {
		t.Fatalf("ListDeploymentRuns: %v", err)
	}
	if len(runsPage.Items) != 2 {
		t.Fatalf("expected 2 tenant-owned runs (filtering out other tenant), got %d", len(runsPage.Items))
	}
	if runsPage.Items[0].TriggerContext.Type != "schedule" || runsPage.Items[1].ID != run.ID {
		t.Fatalf("unexpected runs order/content: %+v", runsPage.Items)
	}

	listed, err := rt.ListDeployments(ctx, scopeA, agentruntime.ListDeploymentsQuery{Limit: 10})
	if err != nil || len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("ListDeployments = (%+v, %v)", listed, err)
	}

	archivedDep, err := rt.ArchiveDeployment(ctx, scopeA, created.ID)
	if err != nil || archivedDep.ArchivedAt == nil {
		t.Fatalf("ArchiveDeployment = (%+v, %v)", archivedDep, err)
	}
	listedAfterArchive, err := rt.ListDeployments(ctx, scopeA, agentruntime.ListDeploymentsQuery{Limit: 10})
	if err != nil || len(listedAfterArchive.Items) != 0 {
		t.Fatalf("expected archived deployment filtered out by default: %+v, %v", listedAfterArchive, err)
	}
}

func containsJSONValue(raw json.RawMessage, target string) bool {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	for _, value := range fields {
		if str, ok := value.(string); ok && str == target {
			return true
		}
	}
	return false
}
