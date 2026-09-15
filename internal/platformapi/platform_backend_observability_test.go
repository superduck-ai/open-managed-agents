package platformapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/observability"

	"github.com/go-chi/chi/v5"
)

type recordingBackend struct {
	scope observability.TenantScope
	bound observability.BoundVariables
}

func (b *recordingBackend) PanelRows(_ context.Context, _ string, bound observability.BoundVariables) ([]observability.Row, error) {
	b.scope = bound.Scope
	b.bound = bound
	current := 4.0
	return []observability.Row{{"current": current, "previous": 2.0, "change_percent": 100.0}}, nil
}

func (b *recordingBackend) TraceListRows(_ context.Context, q observability.TraceListQuery) ([]observability.Row, error) {
	b.scope = q.Bound.Scope
	b.bound = q.Bound
	return nil, nil
}

func (b *recordingBackend) TraceSpans(_ context.Context, q observability.TraceDetailQuery) (observability.TraceSpansResult, error) {
	b.scope = q.Bound.Scope
	b.bound = q.Bound
	return observability.TraceSpansResult{Truncated: true}, nil
}

type stubStore struct {
	agentErr error
}

func (s stubStore) GetAgent(context.Context, string, string) error { return s.agentErr }
func (s stubStore) GetSession(context.Context, string, string) error {
	return nil
}

func TestObservabilityPanelQueryContract(t *testing.T) {
	backend := &recordingBackend{}
	handler := observability.NewHandler(backend, stubStore{}, nil)
	router := observabilityRouter(handler, auth.Principal{OrganizationUUID: "org_1", WorkspaceUUID: "ws_1"})

	t.Run("200", func(t *testing.T) {
		recorder := doPanelQuery(router, `{"query_ref":"overview.interactions","variables":{"start_time":"2026-08-12T00:00:00Z","end_time":"2026-08-13T00:00:00Z"}}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["render_type"] != "stat" || payload["query_ref"] != "overview.interactions" || payload["data_as_of"] == nil {
			t.Fatalf("payload = %#v", payload)
		}
		if backend.scope.OrganizationUUID != "org_1" || backend.scope.WorkspaceUUID != "ws_1" {
			t.Fatalf("scope = %+v", backend.scope)
		}
	})

	t.Run("200-agent-version", func(t *testing.T) {
		recorder := doPanelQuery(router, `{"query_ref":"overview.interactions","variables":{"start_time":"2026-08-12T00:00:00Z","end_time":"2026-08-13T00:00:00Z","agent_version":[3,4]}}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
		}
		if got := backend.scope.AgentVersions; len(got) != 2 || got[0] != 3 || got[1] != 4 {
			t.Fatalf("AgentVersions = %#v", backend.scope.AgentVersions)
		}
	})

	t.Run("400", func(t *testing.T) {
		recorder := doPanelQuery(router, `{"query_ref":"overview.interactions","variables":{"start_time":"2026-08-13T00:00:00Z","end_time":"2026-08-12T00:00:00Z"}}`)
		assertAnthropicError(t, recorder, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("413", func(t *testing.T) {
		body := `{"query_ref":"overview.interactions","variables":{"model":["` + strings.Repeat("x", maxObservabilityPanelQueryBodyBytes) + `"]}}`
		recorder := doPanelQuery(router, body)
		assertAnthropicError(t, recorder, http.StatusRequestEntityTooLarge, "invalid_request_error")
	})

	t.Run("400-unknown-field", func(t *testing.T) {
		recorder := doPanelQuery(router, `{"query_ref":"overview.interactions","variables":{},"extra":true}`)
		assertAnthropicError(t, recorder, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("404-query-ref", func(t *testing.T) {
		recorder := doPanelQuery(router, `{"query_ref":"does.not.exist","variables":{"start_time":"2026-08-12T00:00:00Z","end_time":"2026-08-13T00:00:00Z"}}`)
		assertAnthropicError(t, recorder, http.StatusNotFound, "not_found_error")
	})

	t.Run("404-agent", func(t *testing.T) {
		missing := observability.NewHandler(&recordingBackend{}, stubStore{agentErr: db.ErrNotFound}, nil)
		router := observabilityRouter(missing, auth.Principal{OrganizationUUID: "org_1", WorkspaceUUID: "ws_1"})
		recorder := doPanelQuery(router, `{"query_ref":"overview.interactions","variables":{"start_time":"2026-08-12T00:00:00Z","end_time":"2026-08-13T00:00:00Z","agent_id":"agent_missing"}}`)
		assertAnthropicError(t, recorder, http.StatusNotFound, "not_found_error")
	})
}

func TestObservabilityTraceListAgentVersion(t *testing.T) {
	backend := &recordingBackend{}
	handler := observability.NewHandler(backend, stubStore{}, nil)
	router := observabilityRouter(handler, auth.Principal{OrganizationUUID: "org_1", WorkspaceUUID: "ws_1"})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability/traces?start_time=2026-08-12T00:00:00Z&end_time=2026-08-13T00:00:00Z&agent_version=4&agent_version=3", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := backend.scope.AgentVersions; len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("AgentVersions = %#v", got)
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability/traces?start_time=2026-08-12T00:00:00Z&end_time=2026-08-13T00:00:00Z&agent_version=7,8", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("comma list status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if got := backend.scope.AgentVersions; len(got) != 2 || got[0] != 7 || got[1] != 8 {
		t.Fatalf("comma AgentVersions = %#v", got)
	}
}

func TestObservabilityGetTraceTimeWindow(t *testing.T) {
	backend := &recordingBackend{}
	handler := observability.NewHandler(backend, stubStore{}, nil)
	router := observabilityRouter(handler, auth.Principal{OrganizationUUID: "org_1", WorkspaceUUID: "ws_1"})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability/traces/abc", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail observability.TraceDetailResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil || !detail.Truncated {
		t.Fatalf("detail = %+v err=%v", detail, err)
	}
	if backend.bound.Window.End.Sub(backend.bound.Window.Start) != 30*24*time.Hour+time.Hour {
		t.Fatalf("default detail window = %+v", backend.bound.Window)
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability/traces/abc?start_time=2026-08-12T00:00:00Z&end_time=2026-08-13T00:00:00Z&agent_version=4", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("bounded detail status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !backend.bound.Window.Start.Equal(time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)) || len(backend.bound.Scope.AgentVersions) != 1 || backend.bound.Scope.AgentVersions[0] != 4 {
		t.Fatalf("bounded detail = %+v", backend.bound)
	}

	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability/traces/abc?start_time=2026-08-12T00:00:00Z", nil)
	router.ServeHTTP(recorder, req)
	assertAnthropicError(t, recorder, http.StatusBadRequest, "invalid_request_error")
}

func TestObservabilityMirrorOrganizationScope(t *testing.T) {
	backend := &recordingBackend{}
	handler := observability.NewHandler(backend, stubStore{}, nil)
	router := observabilityRouter(handler, auth.Principal{OrganizationUUID: "local-org", WorkspaceUUID: "ws_1"})
	body := `{"query_ref":"overview.interactions","variables":{"start_time":"2026-08-12T00:00:00Z","end_time":"2026-08-13T00:00:00Z"}}`

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "https://platform.claude.com/api/organizations/official-org/observability/panels/query", strings.NewReader(body))
	req = req.WithContext(auth.WithPlatformMirrorOrganizationAlias(req.Context(), "official-org"))
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || backend.scope.OrganizationUUID != "local-org" {
		t.Fatalf("mirror status = %d scope = %+v body=%s", recorder.Code, backend.scope, recorder.Body.String())
	}

	backend.scope = observability.TenantScope{}
	recorder = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "http://oma.local/api/organizations/official-org/observability/panels/query", strings.NewReader(body))
	req = req.WithContext(auth.WithPlatformMirrorOrganizationAlias(req.Context(), "official-org"))
	router.ServeHTTP(recorder, req)
	assertAnthropicError(t, recorder, http.StatusNotFound, "not_found_error")
	if backend.scope.OrganizationUUID != "" {
		t.Fatalf("rejected mirror reached backend with scope = %+v", backend.scope)
	}
}

// 投影内容（含 sql/stream 不泄漏）由 observability 包的
// TestDashboardProjectionOmitsSQLAndStream 保证；observability 关闭时路由不注册
// （404）由 tests/platform_console_backend_api_test.go 端到端覆盖。
func TestObservabilityDashboardEndpoint(t *testing.T) {
	handler := observability.NewHandler(&recordingBackend{}, stubStore{}, nil)
	router := observabilityRouter(handler, auth.Principal{OrganizationUUID: "org_1", WorkspaceUUID: "ws_1"})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability/dashboard", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Tabs []any `json:"tabs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || len(payload.Tabs) == 0 {
		t.Fatalf("dashboard payload = %s err=%v", recorder.Body.String(), err)
	}
}

func observabilityRouter(provider ObservabilityProvider, principal auth.Principal) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/organizations/{orgUuid}", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(auth.WithPrincipal(req.Context(), principal)))
			})
		})
		RegisterOrganizationObservabilityRoutes(r, provider, nil)
	})
	return r
}

func doPanelQuery(handler http.Handler, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/organizations/org_1/observability/panels/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, req)
	return recorder
}

func assertAnthropicError(t *testing.T, recorder *httptest.ResponseRecorder, status int, errorType string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "error" {
		t.Fatalf("payload = %#v", payload)
	}
	errBody, _ := payload["error"].(map[string]any)
	if errBody["type"] != errorType {
		t.Fatalf("error = %#v", payload["error"])
	}
}
