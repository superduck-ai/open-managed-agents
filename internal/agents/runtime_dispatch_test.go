package agents

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
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type stubAgentProfileStore struct {
	profile db.RuntimeProfile
}

func (s *stubAgentProfileStore) GetRuntimeProfileByUUID(context.Context, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubAgentProfileStore) GetWorkspaceRuntimeProfile(context.Context, string, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubAgentProfileStore) UpdateRuntimeProfileHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

type mockAgentRuntime struct {
	createdInput agentruntime.CreateAgentInput
	searchQuery  agentruntime.SearchAgentsQuery
	updatedInput agentruntime.UpdateAgentInput
	archivedID   string
	agent        agentruntime.Agent
}

func (m *mockAgentRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (m *mockAgentRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (m *mockAgentRuntime) Close() error { return nil }

func (m *mockAgentRuntime) CreateAgent(_ context.Context, _ agentruntime.Scope, input agentruntime.CreateAgentInput) (agentruntime.Agent, error) {
	m.createdInput = input
	return m.agent, nil
}

func (m *mockAgentRuntime) GetAgent(context.Context, agentruntime.Scope, string) (agentruntime.Agent, error) {
	return m.agent, nil
}

func (m *mockAgentRuntime) ListAgents(context.Context, agentruntime.Scope, agentruntime.ListAgentsQuery) (agentruntime.Page[agentruntime.Agent], error) {
	return agentruntime.Page[agentruntime.Agent]{Items: []agentruntime.Agent{m.agent}}, nil
}

func (m *mockAgentRuntime) SearchAgents(_ context.Context, _ agentruntime.Scope, query agentruntime.SearchAgentsQuery) (agentruntime.Page[agentruntime.Agent], error) {
	m.searchQuery = query
	return agentruntime.Page[agentruntime.Agent]{Items: []agentruntime.Agent{m.agent}}, nil
}

func (m *mockAgentRuntime) UpdateAgent(_ context.Context, _ agentruntime.Scope, _ string, input agentruntime.UpdateAgentInput) (agentruntime.Agent, error) {
	m.updatedInput = input
	updated := m.agent
	if input.Name != nil {
		updated.Name = *input.Name
	}
	updated.Version = 2
	return updated, nil
}

func (m *mockAgentRuntime) ArchiveAgent(_ context.Context, _ agentruntime.Scope, agentID string) (agentruntime.Agent, error) {
	m.archivedID = agentID
	archived := m.agent
	now := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)
	archived.ArchivedAt = &now
	return archived, nil
}

func TestAgentsHandlerExternalRuntimeDispatch(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	mockRT := &mockAgentRuntime{
		agent: agentruntime.Agent{
			ID:        "agent_qdr_123",
			Version:   1,
			Name:      "Qoder Coder",
			Model:     "qmodel_latest",
			System:    "Be concise",
			Tools:     json.RawMessage(`[{"type":"agent_toolset_20260401"}]`),
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
	resolver := agentruntime.NewResolver(&stubAgentProfileStore{profile: profile}, registry, nil, nil)
	handler := NewHandler(nil, nil, nil).WithRuntimeResolver(resolver)

	withPrincipal := func(req *http.Request) *http.Request {
		return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID:   "00000000-0000-4000-8000-000000000010",
			WorkspaceUUID:      "00000000-0000-4000-8000-000000000011",
			RuntimeProvider:    agentruntime.ProviderQoderCloud.String(),
			RuntimeProfileUUID: profile.UUID,
			CredentialType:     auth.CredentialTypeAPIKey,
		}))
	}

	t.Run("create skips CanonicalModelID validation for external models", func(t *testing.T) {
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Qoder Coder","model":"qmodel_latest","system":"Be concise"}`)))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		if mockRT.createdInput.Model != "qmodel_latest" || mockRT.createdInput.Name != "Qoder Coder" {
			t.Fatalf("unexpected create input: %+v", mockRT.createdInput)
		}
		var resp agentResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.ID != "agent_qdr_123" || resp.Type != "agent" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("get list search update archive", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/agent_qdr_123", nil)))
		if rec.Code != http.StatusOK {
			t.Fatalf("get status = %d: %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/?limit=10", nil)))
		if rec.Code != http.StatusOK {
			t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.Search(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/agents:search", strings.NewReader(`{"name":"Qoder"}`))))
		if rec.Code != http.StatusOK || mockRT.searchQuery.Query != "Qoder" {
			t.Fatalf("search status = %d query = %+v body = %s", rec.Code, mockRT.searchQuery, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/agent_qdr_123", strings.NewReader(`{"name":"Renamed","version":1}`))))
		if rec.Code != http.StatusOK || mockRT.updatedInput.Name == nil || *mockRT.updatedInput.Name != "Renamed" {
			t.Fatalf("update status = %d input = %+v body = %s", rec.Code, mockRT.updatedInput, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/agent_qdr_123/archive", nil)))
		if rec.Code != http.StatusOK || mockRT.archivedID != "agent_qdr_123" {
			t.Fatalf("archive status = %d archivedID = %q body = %s", rec.Code, mockRT.archivedID, rec.Body.String())
		}
	})
}
