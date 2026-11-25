package channels

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func routePatterns(t *testing.T, router http.Handler) map[string]struct{} {
	t.Helper()
	patterns := make(map[string]struct{})
	if err := chi.Walk(router.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		patterns[method+" "+route] = struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return patterns
}

func TestWorkspaceRouterServesChannelQr(t *testing.T) {
	handler := newLocalHandler(testLocalService(&fakeRegistrationStore{records: map[string]db.ChannelRegistrationRecord{}}, testCipher(t), nil, nil), nil)
	patterns := routePatterns(t, handler.workspaceRouter())
	for _, route := range []string{"GET /{channel_id}/qr", "POST /{channel_id}/qr", "GET /authorizations/{authorization_id}", "POST /authorizations"} {
		if _, ok := patterns[route]; !ok {
			t.Fatalf("工作区路由缺少 %s：%v", route, patterns)
		}
	}
}

func TestAgentRouterNoLongerServesChannelQr(t *testing.T) {
	handler := newLocalHandler(testLocalService(&fakeRegistrationStore{records: map[string]db.ChannelRegistrationRecord{}}, testCipher(t), nil, nil), nil)
	patterns := routePatterns(t, handler.agentRouter())
	if _, ok := patterns["GET /{channel_id}/qr"]; ok {
		t.Fatalf("扫码不应再挂在 agent 路由：%v", patterns)
	}
}

func TestConsoleOnlyChannelRoutesRejectAPIKey(t *testing.T) {
	handler := newLocalHandler(testLocalService(&fakeLocalStore{}, nil, nil, nil), nil)
	agent := chi.NewRouter()
	agent.Mount("/agents/{agent_id}/channels", handler.agentRouter())
	workspace := chi.NewRouter()
	workspace.Mount("/channels", handler.workspaceRouter())
	routes := []struct {
		method string
		path   string
		router http.Handler
	}{
		{http.MethodGet, "/agents/agt/channels/settings", agent},
		{http.MethodPost, "/agents/agt/channels/settings", agent},
		{http.MethodDelete, "/agents/agt/channels/chn", agent},
		{http.MethodGet, "/agents/agt/channels/chn/pairings", agent},
		{http.MethodPost, "/agents/agt/channels/chn/pairings/pair", agent},
		{http.MethodGet, "/channels/chn/qr", workspace},
		{http.MethodPost, "/channels/chn/qr", workspace},
		{http.MethodPost, "/channels/chn/qr_sessions", workspace},
		{http.MethodGet, "/channels/chn/qr_sessions/qr", workspace},
	}
	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			request := httptest.NewRequest(route.method, route.path, nil)
			request = request.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{
				OrganizationUUID: "org",
				WorkspaceUUID:    "ws",
				CredentialType:   auth.CredentialTypeAPIKey,
			}))
			response := httptest.NewRecorder()
			route.router.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"type":"not_found_error"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/agents/agt/channels/settings", nil)
	request = request.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{
		OrganizationUUID: "org",
		WorkspaceUUID:    "ws",
		CredentialType:   auth.CredentialTypePlatformSession,
	}))
	response := httptest.NewRecorder()
	agent.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("平台会话应继续进入执行环境读取，status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNativeChannelMutationRejectsMissingScopeAndCSRF(t *testing.T) {
	for _, tc := range []struct {
		name      string
		principal auth.Principal
		want      int
	}{
		{name: "无身份", want: http.StatusUnauthorized},
		{name: "缺少工作区", principal: auth.Principal{OrganizationUUID: "org"}, want: http.StatusUnauthorized},
		{name: "浏览器缺少CSRF", principal: auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws", CredentialType: auth.CredentialTypePlatformSession}, want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newLocalHandler(testLocalService(&fakeLocalStore{}, nil, nil, nil), nil)
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"渠道"}`))
			request = request.WithContext(auth.WithPrincipal(t.Context(), tc.principal))
			response := httptest.NewRecorder()
			handler.agentRouter().ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d", response.Code, tc.want)
			}
		})
	}
}

func TestNativeChannelRoutesRejectForeignOrArchivedAgent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		store *fakeLocalStore
		want  int
	}{
		{"其他工作区智能体", &fakeLocalStore{agentErr: db.ErrNotFound}, http.StatusNotFound},
		{"归档智能体", &fakeLocalStore{agent: db.Agent{ArchivedAt: new(time.Now())}}, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newLocalHandler(testLocalService(tc.store, nil, nil, nil), nil)
			router := chi.NewRouter()
			router.Mount("/agents/{agent_id}/channels", handler.agentRouter())
			request := httptest.NewRequest(http.MethodPost, "/agents/agent/channels", strings.NewReader(`{"name":"渠道"}`))
			request = request.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws", CredentialType: auth.CredentialTypeAPIKey}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d want=%d", response.Code, tc.want)
			}
		})
	}
}

func TestChannelSaveReportsAppAlreadyBound(t *testing.T) {
	store := &fakeLocalStore{
		createErr: db.ErrChannelAppAlreadyBound,
		updateErr: db.ErrChannelAppAlreadyBound,
		records:   []db.ChannelRecord{{ExternalID: "chnl_existing"}},
	}
	handler := newLocalHandler(testLocalService(store, nil, nil, nil), nil)
	router := chi.NewRouter()
	router.Mount("/agents/{agent_id}/channels", handler.agentRouter())
	for _, path := range []string{"/agents/agent/channels", "/agents/agent/channels/chnl_existing"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), auth.Principal{
				OrganizationUUID: "org",
				WorkspaceUUID:    "ws",
				CredentialType:   auth.CredentialTypePlatformSession,
			}), http.MethodPost, path, strings.NewReader(`{"name":"渠道"}`))
			request.AddCookie(&http.Cookie{Name: "sessionKey", Value: "test-session"})
			request.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken("test-session"))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusConflict ||
				!strings.Contains(response.Body.String(), `"type":"conflict_error"`) ||
				!strings.Contains(response.Body.String(), "飞书 App ID 已绑定到其他渠道，请使用未绑定的飞书应用。") {
				t.Fatalf("渠道保存应返回应用绑定冲突，status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
