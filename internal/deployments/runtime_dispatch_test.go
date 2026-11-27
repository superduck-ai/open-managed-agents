package deployments

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type fakeDeploymentProfileStore struct {
	profile db.RuntimeProfile
}

func (f *fakeDeploymentProfileStore) GetRuntimeProfileByUUID(_ context.Context, _ string) (db.RuntimeProfile, error) {
	return f.profile, nil
}

func (f *fakeDeploymentProfileStore) GetWorkspaceRuntimeProfile(_ context.Context, _, _ string) (db.RuntimeProfile, error) {
	return f.profile, nil
}

func (f *fakeDeploymentProfileStore) UpdateRuntimeProfileHealth(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}

type fakeExternalDeploymentRuntime struct {
	createInput agentruntime.CreateDeploymentInput
	updateInput agentruntime.UpdateDeploymentInput
	listQuery   agentruntime.ListDeploymentsQuery
	runsQuery   agentruntime.ListDeploymentRunsQuery
	deployment  agentruntime.Deployment
	run         agentruntime.DeploymentRun
}

func (f *fakeExternalDeploymentRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (f *fakeExternalDeploymentRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (f *fakeExternalDeploymentRuntime) Close() error { return nil }

func (f *fakeExternalDeploymentRuntime) CreateDeployment(_ context.Context, _ agentruntime.Scope, input agentruntime.CreateDeploymentInput) (agentruntime.Deployment, error) {
	f.createInput = input
	return f.deployment, nil
}

func (f *fakeExternalDeploymentRuntime) GetDeployment(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.Deployment, error) {
	return f.deployment, nil
}

func (f *fakeExternalDeploymentRuntime) ListDeployments(_ context.Context, _ agentruntime.Scope, query agentruntime.ListDeploymentsQuery) (agentruntime.Page[agentruntime.Deployment], error) {
	f.listQuery = query
	return agentruntime.Page[agentruntime.Deployment]{Items: []agentruntime.Deployment{f.deployment}, HasMore: true}, nil
}

func (f *fakeExternalDeploymentRuntime) UpdateDeployment(_ context.Context, _ agentruntime.Scope, _ string, input agentruntime.UpdateDeploymentInput) (agentruntime.Deployment, error) {
	f.updateInput = input
	updated := f.deployment
	if input.Name != nil {
		updated.Name = *input.Name
	}
	return updated, nil
}

func (f *fakeExternalDeploymentRuntime) ArchiveDeployment(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.Deployment, error) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	archived := f.deployment
	archived.ArchivedAt = &now
	return archived, nil
}

func (f *fakeExternalDeploymentRuntime) PauseDeployment(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.Deployment, error) {
	paused := f.deployment
	paused.Status = "paused"
	paused.PausedReason = &agentruntime.DeploymentPausedReason{Type: "manual"}
	return paused, nil
}

func (f *fakeExternalDeploymentRuntime) UnpauseDeployment(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.Deployment, error) {
	active := f.deployment
	active.Status = "active"
	return active, nil
}

func (f *fakeExternalDeploymentRuntime) RunDeployment(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.DeploymentRun, error) {
	return f.run, nil
}

func (f *fakeExternalDeploymentRuntime) GetDeploymentRun(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.DeploymentRun, error) {
	return f.run, nil
}

func (f *fakeExternalDeploymentRuntime) ListDeploymentRuns(_ context.Context, _ agentruntime.Scope, query agentruntime.ListDeploymentRunsQuery) (agentruntime.Page[agentruntime.DeploymentRun], error) {
	f.runsQuery = query
	return agentruntime.Page[agentruntime.DeploymentRun]{Items: []agentruntime.DeploymentRun{f.run}, HasMore: false}, nil
}

func TestExternalDeploymentsAndRunsDispatch(t *testing.T) {
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	sessionID := "sesn_ext_1"
	fakeRT := &fakeExternalDeploymentRuntime{
		deployment: agentruntime.Deployment{
			ID:            "depl_ext_1",
			Agent:         agentruntime.DeploymentAgentRef{ID: "agent_ext_1", Type: "agent", Version: 1},
			EnvironmentID: "env_ext_1",
			Name:          "Morning Sync",
			Metadata:      map[string]string{"env": "prod"},
			Budget:        &agentruntime.SessionBudget{Mode: agentruntime.BudgetModeCredits, MaxCreditCost: "25.00"},
			Status:        "active",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		run: agentruntime.DeploymentRun{
			ID:             "drun_ext_1",
			DeploymentID:   "depl_ext_1",
			Agent:          agentruntime.DeploymentAgentRef{ID: "agent_ext_1", Type: "agent", Version: 1},
			SessionID:      &sessionID,
			TriggerContext: agentruntime.DeploymentRunTriggerContext{Type: "manual"},
			CreatedAt:      now,
		},
	}

	reg := agentruntime.NewRegistry()
	if err := reg.Register(agentruntime.ProviderQoderCloud, func(_ context.Context, _ agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return fakeRT, nil
	}); err != nil {
		t.Fatal(err)
	}
	resolver := agentruntime.NewResolver(&fakeDeploymentProfileStore{
		profile: db.RuntimeProfile{
			UUID:         "00000000-0000-4000-8000-000000000055",
			ProviderKind: agentruntime.ProviderQoderCloud.String(),
			Status:       "active",
		},
	}, reg, nil, nil)

	depHandler := NewHandler(nil, nil, nil, nil, nil).WithRuntimeResolver(resolver)
	runsHandler := NewRunsHandler(nil, nil).WithRuntimeResolver(resolver)

	principal := auth.Principal{
		OrganizationUUID: "org-1",
		WorkspaceUUID:    "ws-1",
		CredentialType:   auth.CredentialTypeAPIKey,
		RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
	}

	doReq := func(handler http.Handler, method, target string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, target, &buf)
		req.Header.Set("anthropic-version", defaultAnthropicAPIVersion)
		req.Header.Set("anthropic-beta", managedAgentsBeta)
		req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := doReq(depHandler, http.MethodPost, "/", map[string]any{
		"agent":          "agent_ext_1",
		"environment_id": "env_ext_1",
		"name":           "Morning Sync",
		"initial_events": []map[string]any{
			{"type": "user.message", "content": []map[string]any{{"type": "text", "text": "Summarize today"}}},
		},
		"budget": map[string]any{"type": "limit", "max_credit_cost": "25.00"},
		"resources": []map[string]any{
			{"type": "memory_store", "memory_store_id": "memstore_1", "access": "read_write"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	if fakeRT.createInput.Budget == nil || fakeRT.createInput.Budget.MaxCreditCost != "25.00" {
		t.Fatalf("create budget = %+v", fakeRT.createInput.Budget)
	}
	if len(fakeRT.createInput.Resources) != 1 || fakeRT.createInput.Resources[0].MemoryStoreID != "memstore_1" {
		t.Fatalf("create resources = %+v", fakeRT.createInput.Resources)
	}

	rec = doReq(depHandler, http.MethodGet, "/?limit=10&status=active", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", rec.Code, rec.Body.String())
	}
	var listResp pageResponse[deploymentResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil || len(listResp.Data) != 1 || listResp.NextPage == nil {
		t.Fatalf("unexpected list response: %s", rec.Body.String())
	}

	rec = doReq(depHandler, http.MethodPost, "/depl_ext_1", map[string]any{
		"name":     "Morning Sync v2",
		"metadata": map[string]any{"env": nil, "tier": "gold"},
		"budget":   nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !fakeRT.updateInput.BudgetSet || fakeRT.updateInput.Budget != nil {
		t.Fatalf("expected budget cleared: %+v", fakeRT.updateInput)
	}
	if fakeRT.updateInput.Metadata["tier"] != "gold" || fakeRT.updateInput.Metadata["env"] != "" {
		t.Fatalf("unexpected patched metadata: %+v", fakeRT.updateInput.Metadata)
	}

	for _, action := range []string{"/depl_ext_1/pause", "/depl_ext_1/unpause", "/depl_ext_1/archive"} {
		rec = doReq(depHandler, http.MethodPost, action, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", action, rec.Code, rec.Body.String())
		}
	}

	rec = doReq(depHandler, http.MethodPost, "/depl_ext_1/run", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d body = %s", rec.Code, rec.Body.String())
	}
	var runResp deploymentRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &runResp); err != nil || runResp.ID != "drun_ext_1" || runResp.SessionID == nil || *runResp.SessionID != "sesn_ext_1" {
		t.Fatalf("unexpected run response: %s", rec.Body.String())
	}

	rec = doReq(runsHandler, http.MethodGet, "/?deployment_id=depl_ext_1&trigger_type=manual", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list runs status = %d body = %s", rec.Code, rec.Body.String())
	}
	if fakeRT.runsQuery.DeploymentID != "depl_ext_1" || fakeRT.runsQuery.TriggerType != "manual" {
		t.Fatalf("unexpected runs query: %+v", fakeRT.runsQuery)
	}

	rec = doReq(runsHandler, http.MethodGet, "/drun_ext_1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get run status = %d body = %s", rec.Code, rec.Body.String())
	}
}
