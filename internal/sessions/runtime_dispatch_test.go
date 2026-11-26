package sessions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type stubSessionProfileStore struct {
	profile db.RuntimeProfile
}

func (s *stubSessionProfileStore) GetRuntimeProfileByUUID(context.Context, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubSessionProfileStore) GetWorkspaceRuntimeProfile(context.Context, string, string) (db.RuntimeProfile, error) {
	return s.profile, nil
}

func (s *stubSessionProfileStore) UpdateRuntimeProfileHealth(context.Context, string, string, string, time.Time) error {
	return nil
}

type mockEventStream struct {
	frames []agentruntime.StreamFrame
	idx    int
}

func (s *mockEventStream) Next(context.Context) (agentruntime.StreamFrame, error) {
	if s.idx >= len(s.frames) {
		return agentruntime.StreamFrame{}, io.EOF
	}
	frame := s.frames[s.idx]
	s.idx++
	return frame, nil
}

func (s *mockEventStream) Close() error { return nil }

type mockSessionRuntime struct {
	session      agentruntime.Session
	sendCalls    int
	streamOpts   agentruntime.StreamOptions
	streamFrames []agentruntime.StreamFrame
	resources    []agentruntime.SessionResource
	threads      []agentruntime.Thread
}

func (m *mockSessionRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (m *mockSessionRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (m *mockSessionRuntime) Close() error { return nil }

func (m *mockSessionRuntime) CreateSession(_ context.Context, _ agentruntime.Scope, input agentruntime.CreateSessionInput) (agentruntime.Session, error) {
	created := m.session
	created.AgentID = input.AgentID
	created.EnvironmentID = input.EnvironmentID
	created.Budget = input.Budget
	return created, nil
}

func (m *mockSessionRuntime) GetSession(context.Context, agentruntime.Scope, string) (agentruntime.Session, error) {
	return m.session, nil
}

func (m *mockSessionRuntime) ListSessions(context.Context, agentruntime.Scope, agentruntime.ListSessionsQuery) (agentruntime.Page[agentruntime.Session], error) {
	return agentruntime.Page[agentruntime.Session]{Items: []agentruntime.Session{m.session}}, nil
}

func (m *mockSessionRuntime) UpdateSession(_ context.Context, _ agentruntime.Scope, _ string, input agentruntime.UpdateSessionInput) (agentruntime.Session, error) {
	updated := m.session
	if input.Title != nil {
		updated.Title = *input.Title
	}
	return updated, nil
}

func (m *mockSessionRuntime) ArchiveSession(context.Context, agentruntime.Scope, string) (agentruntime.Session, error) {
	archived := m.session
	now := time.Date(2026, 11, 25, 1, 0, 0, 0, time.UTC)
	archived.ArchivedAt = &now
	return archived, nil
}

func (m *mockSessionRuntime) DeleteSession(context.Context, agentruntime.Scope, string) error {
	return nil
}

func (m *mockSessionRuntime) SendEvents(context.Context, agentruntime.Scope, string, agentruntime.SendEventsInput) ([]agentruntime.Event, error) {
	m.sendCalls++
	return []agentruntime.Event{{
		ID:      "sevt_qdr_1",
		Type:    "user.message",
		Payload: json.RawMessage(`{"id":"sevt_qdr_1","type":"user.message"}`),
	}}, nil
}

func (m *mockSessionRuntime) ListEvents(context.Context, agentruntime.Scope, string, agentruntime.ListEventsQuery) (agentruntime.Page[agentruntime.Event], error) {
	return agentruntime.Page[agentruntime.Event]{
		Items: []agentruntime.Event{{
			ID:      "sevt_qdr_1",
			Type:    "agent.message",
			Payload: json.RawMessage(`{"id":"sevt_qdr_1","type":"agent.message"}`),
		}},
	}, nil
}

func (m *mockSessionRuntime) StreamEvents(_ context.Context, _ agentruntime.Scope, _ string, opts agentruntime.StreamOptions) (agentruntime.EventStream, error) {
	m.streamOpts = opts
	return &mockEventStream{frames: m.streamFrames}, nil
}

func (m *mockSessionRuntime) ListSessionResources(context.Context, agentruntime.Scope, string) ([]agentruntime.SessionResource, error) {
	return m.resources, nil
}

func (m *mockSessionRuntime) AddSessionResource(_ context.Context, _ agentruntime.Scope, sessionID string, input agentruntime.AddSessionResourceInput) (agentruntime.SessionResource, error) {
	res := agentruntime.SessionResource{
		ID:        "sesrsc_qdr_1",
		SessionID: sessionID,
		Type:      input.Type,
		FileID:    input.FileID,
		MountPath: input.MountPath,
		CreatedAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
	}
	m.resources = append(m.resources, res)
	return res, nil
}

func (m *mockSessionRuntime) UpdateSessionResource(_ context.Context, _ agentruntime.Scope, sessionID string, resourceID string, input agentruntime.UpdateSessionResourceInput) (agentruntime.SessionResource, error) {
	mountPath := ""
	if input.MountPath != nil {
		mountPath = *input.MountPath
	}
	return agentruntime.SessionResource{
		ID:        resourceID,
		SessionID: sessionID,
		Type:      "file",
		MountPath: mountPath,
		CreatedAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (m *mockSessionRuntime) DeleteSessionResource(context.Context, agentruntime.Scope, string, string) error {
	return nil
}

func (m *mockSessionRuntime) ListThreads(context.Context, agentruntime.Scope, string) ([]agentruntime.Thread, error) {
	return m.threads, nil
}

func (m *mockSessionRuntime) GetThread(context.Context, agentruntime.Scope, string, string) (agentruntime.Thread, error) {
	return m.threads[0], nil
}

func TestSessionsHandlerExternalRuntimeDispatch(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	mockRT := &mockSessionRuntime{
		session: agentruntime.Session{
			ID:            "sesn_qdr_1",
			AgentID:       "agent_qdr_1",
			AgentVersion:  1,
			EnvironmentID: "env_qdr_1",
			Title:         "Qoder Session",
			Status:        "idle",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		streamFrames: []agentruntime.StreamFrame{
			{ID: "sevt_qdr_10", Event: "agent.message", Data: json.RawMessage(`{"id":"sevt_qdr_10","type":"agent.message"}`)},
			{ID: "sevt_qdr_11", Event: "session.status_idle", Data: json.RawMessage(`{"id":"sevt_qdr_11","type":"session.status_idle"}`)},
		},
		threads: []agentruntime.Thread{{
			ID:        "sthr_qdr_1",
			SessionID: "sesn_qdr_1",
			Status:    "idle",
			CreatedAt: now,
			UpdatedAt: now,
		}},
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
	resolver := agentruntime.NewResolver(&stubSessionProfileStore{profile: profile}, registry, nil, nil)
	codeSessionSvc := codesessions.NewServiceWithCredentials(nil, new(codesessions.SessionCredentials), nil)
	handler := NewHandler(config.Config{}, nil, codeSessionSvc, nil, nil, nil, nil).WithRuntimeResolver(resolver)

	withPrincipal := func(req *http.Request) *http.Request {
		return req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
			OrganizationUUID:   "00000000-0000-4000-8000-000000000010",
			WorkspaceUUID:      "00000000-0000-4000-8000-000000000011",
			RuntimeProvider:    agentruntime.ProviderQoderCloud.String(),
			RuntimeProfileUUID: profile.UUID,
			CredentialType:     auth.CredentialTypeAPIKey,
		}))
	}

	t.Run("create rejects agent_with_overrides and accepts credits budget", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"agent":{"id":"agent_qdr_1","type":"agent_with_overrides"},"environment_id":"env_qdr_1"}`)))
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("override status = %d, want 400: %s", rec.Code, rec.Body.String())
		}

		rec = httptest.NewRecorder()
		req = withPrincipal(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"agent":"agent_qdr_1","environment_id":"env_qdr_1","budget":{"type":"limit","max_credit_cost":"50.00"}}`)))
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("create status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("ports preview returns 400 unsupported_capability_error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/sesn_qdr_1/ports/3000", nil))
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("ports status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "unsupported_capability_error") {
			t.Fatalf("expected unsupported_capability_error, got: %s", rec.Body.String())
		}
	})

	t.Run("events send deduplicates via Idempotency-Key and rejects mismatch with 409", func(t *testing.T) {
		payload := `{"events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`
		req1 := withPrincipal(httptest.NewRequest(http.MethodPost, "/sesn_qdr_1/events", strings.NewReader(payload)))
		req1.Header.Set("Idempotency-Key", "idem-key-1")
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK || mockRT.sendCalls != 1 {
			t.Fatalf("first send status=%d calls=%d body=%s", rec1.Code, mockRT.sendCalls, rec1.Body.String())
		}

		req2 := withPrincipal(httptest.NewRequest(http.MethodPost, "/sesn_qdr_1/events", strings.NewReader(payload)))
		req2.Header.Set("Idempotency-Key", "idem-key-1")
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK || mockRT.sendCalls != 1 {
			t.Fatalf("duplicate send status=%d calls=%d body=%s", rec2.Code, mockRT.sendCalls, rec2.Body.String())
		}

		req3 := withPrincipal(httptest.NewRequest(http.MethodPost, "/sesn_qdr_1/events", strings.NewReader(`{"events":[{"type":"user.message","content":[{"type":"text","text":"different"}]}]}`)))
		req3.Header.Set("Idempotency-Key", "idem-key-1")
		rec3 := httptest.NewRecorder()
		handler.ServeHTTP(rec3, req3)
		if rec3.Code != http.StatusConflict {
			t.Fatalf("conflict send status=%d, want 409: %s", rec3.Code, rec3.Body.String())
		}
	})

	t.Run("sse stream flushes external frames and forwards Last-Event-ID", func(t *testing.T) {
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/sesn_qdr_1/events/stream?event_deltas[]=agent.message", nil))
		req.Header.Set("Last-Event-ID", "sevt_qdr_09")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("stream status = %d: %s", rec.Code, rec.Body.String())
		}
		if mockRT.streamOpts.LastEventID != "sevt_qdr_09" {
			t.Fatalf("LastEventID = %q, want sevt_qdr_09", mockRT.streamOpts.LastEventID)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "id: sevt_qdr_10\nevent: agent.message\n") || !strings.Contains(body, "event: session.status_idle\n") {
			t.Fatalf("unexpected SSE body: %s", body)
		}
	})

	t.Run("session crud resources and threads", func(t *testing.T) {
		for _, tc := range []struct {
			method string
			path   string
			body   string
			header map[string]string
		}{
			{http.MethodGet, "/", "", nil},
			{http.MethodGet, "/sesn_qdr_1", "", nil},
			{http.MethodPost, "/sesn_qdr_1", `{"title":"Updated"}`, nil},
			{http.MethodGet, "/sesn_qdr_1/events", "", nil},
			{http.MethodPost, "/sesn_qdr_1/resources", `{"type":"file","file_id":"file_qdr_1","mount_path":"/data/a.txt"}`, nil},
			{http.MethodGet, "/sesn_qdr_1/resources", "", nil},
			{http.MethodGet, "/sesn_qdr_1/resources/sesrsc_qdr_1", "", nil},
			{http.MethodPost, "/sesn_qdr_1/resources/sesrsc_qdr_1", `{"mount_path":"/data/b.txt"}`, nil},
			{http.MethodDelete, "/sesn_qdr_1/resources/sesrsc_qdr_1", "", nil},
			{http.MethodGet, "/sesn_qdr_1/threads", "", map[string]string{"anthropic-beta": managedThreadBeta}},
			{http.MethodGet, "/sesn_qdr_1/threads/sthr_qdr_1", "", map[string]string{"anthropic-beta": managedThreadBeta}},
			{http.MethodPost, "/sesn_qdr_1/archive", "", nil},
			{http.MethodDelete, "/sesn_qdr_1", "", nil},
		} {
			req := withPrincipal(httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want 200: %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		}
	})
}
