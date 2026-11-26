package models

import (
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

type stubModelProfileStore struct {
	profile db.RuntimeProfile
}

func (s *stubModelProfileStore) GetRuntimeProfileByUUID(context.Context, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubModelProfileStore) GetWorkspaceRuntimeProfile(context.Context, string, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubModelProfileStore) UpdateRuntimeProfileHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

type mockModelRuntime struct {
	models []agentruntime.Model
}

func (m *mockModelRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (m *mockModelRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (m *mockModelRuntime) Close() error { return nil }

func (m *mockModelRuntime) ListModels(context.Context, agentruntime.Scope) ([]agentruntime.Model, error) {
	return m.models, nil
}

func TestModelsHandlerExternalRuntimeDispatch(t *testing.T) {
	registry := agentruntime.NewRegistry()
	if err := registry.Register(agentruntime.ProviderQoderCloud, func(context.Context, agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return &mockModelRuntime{
			models: []agentruntime.Model{
				{ID: "qmodel_latest", DisplayName: "Qoder Latest", DefaultContextWindow: 200000, Enabled: true, ProviderKind: agentruntime.ProviderQoderCloud},
				{ID: "fast", DisplayName: "Qoder Fast", DefaultContextWindow: 128000, Enabled: true, ProviderKind: agentruntime.ProviderQoderCloud},
			},
		}, nil
	}); err != nil {
		t.Fatalf("register factory: %v", err)
	}
	profile := db.RuntimeProfile{
		UUID:         "00000000-0000-4000-8000-000000000090",
		ExternalID:   "rprof_qoder_cn",
		ProviderKind: agentruntime.ProviderQoderCloud.String(),
		Status:       "active",
	}
	resolver := agentruntime.NewResolver(&stubModelProfileStore{profile: profile}, registry, nil, nil)
	handler := NewHandler(nil).WithRuntimeResolver(resolver)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
		OrganizationUUID:   "00000000-0000-4000-8000-000000000010",
		WorkspaceUUID:      "00000000-0000-4000-8000-000000000011",
		RuntimeProvider:    agentruntime.ProviderQoderCloud.String(),
		RuntimeProfileUUID: profile.UUID,
		CredentialType:     auth.CredentialTypeAPIKey,
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp listResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Data) != 2 || resp.Data[0].ID != "qmodel_latest" || resp.Data[0].MaxInputTokens == nil || *resp.Data[0].MaxInputTokens != 200000 {
		t.Fatalf("unexpected models response: %+v", resp)
	}
	if resp.FirstID == nil || *resp.FirstID != "qmodel_latest" || resp.LastID == nil || *resp.LastID != "fast" {
		t.Fatalf("unexpected cursors: %+v", resp)
	}
}
