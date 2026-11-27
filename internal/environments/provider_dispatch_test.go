package environments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type stubEnvProfileStore struct {
	profile db.RuntimeProfile
}

func (s *stubEnvProfileStore) GetRuntimeProfileByUUID(context.Context, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubEnvProfileStore) GetWorkspaceRuntimeProfile(context.Context, string, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubEnvProfileStore) UpdateRuntimeProfileHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

type mockEnvRuntime struct {
	env       agentruntime.Environment
	deletedID string
}

func (m *mockEnvRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (m *mockEnvRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (m *mockEnvRuntime) Close() error { return nil }

func (m *mockEnvRuntime) CreateEnvironment(_ context.Context, _ agentruntime.Scope, input agentruntime.CreateEnvironmentInput) (agentruntime.Environment, error) {
	created := m.env
	created.Name = input.Name
	return created, nil
}

func (m *mockEnvRuntime) GetEnvironment(context.Context, agentruntime.Scope, string) (agentruntime.Environment, error) {
	return m.env, nil
}

func (m *mockEnvRuntime) ListEnvironments(context.Context, agentruntime.Scope, agentruntime.ListEnvironmentsQuery) (agentruntime.Page[agentruntime.Environment], error) {
	return agentruntime.Page[agentruntime.Environment]{Items: []agentruntime.Environment{m.env}}, nil
}

func (m *mockEnvRuntime) UpdateEnvironment(_ context.Context, _ agentruntime.Scope, _ string, input agentruntime.UpdateEnvironmentInput) (agentruntime.Environment, error) {
	updated := m.env
	if input.Name != nil {
		updated.Name = *input.Name
	}
	return updated, nil
}

func (m *mockEnvRuntime) ArchiveEnvironment(context.Context, agentruntime.Scope, string) (agentruntime.Environment, error) {
	archived := m.env
	now := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)
	archived.ArchivedAt = &now
	return archived, nil
}

func (m *mockEnvRuntime) DeleteEnvironment(_ context.Context, _ agentruntime.Scope, environmentID string) error {
	m.deletedID = environmentID
	return nil
}

func TestEnvironmentsHandlerExternalRuntimeDispatch(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	mockRT := &mockEnvRuntime{
		env: agentruntime.Environment{
			ID:        "env_qdr_1",
			Name:      "Qoder Cloud Env",
			Config:    json.RawMessage(`{"type":"cloud"}`),
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	registry := agentruntime.NewRegistry()
	if err := registry.Register(agentruntime.ProviderQoderCloud, func(context.Context, agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return mockRT, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	profile := db.RuntimeProfile{
		UUID:         "00000000-0000-4000-8000-000000000090",
		ProviderKind: agentruntime.ProviderQoderCloud.String(),
		Status:       "active",
	}
	resolver := agentruntime.NewResolver(&stubEnvProfileStore{profile: profile}, registry, nil, nil)
	handler := NewHandler(config.Config{}, nil, nil).WithRuntimeResolver(resolver)

	withPrincipal := func(req *http.Request) *http.Request {
		return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID:   "00000000-0000-4000-8000-000000000010",
			WorkspaceUUID:      "00000000-0000-4000-8000-000000000011",
			RuntimeProvider:    agentruntime.ProviderQoderCloud.String(),
			RuntimeProfileUUID: profile.UUID,
			CredentialType:     auth.CredentialTypeAPIKey,
		}))
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/?beta=true", strings.NewReader(`{"name":"Qoder Cloud Env","config":{"type":"cloud"}}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/?beta=true", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/env_qdr_1?beta=true", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/env_qdr_1?beta=true", strings.NewReader(`{"name":"Updated Env"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/env_qdr_1/archive?beta=true", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status = %d: %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodDelete, "/env_qdr_1?beta=true", nil)))
	if rec.Code != http.StatusOK || mockRT.deletedID != "env_qdr_1" {
		t.Fatalf("delete status = %d deletedID = %q: %s", rec.Code, mockRT.deletedID, rec.Body.String())
	}
}
