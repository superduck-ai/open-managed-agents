package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestPlatformIdentityMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		cookie string
		csrf   string
		status int
	}{
		{"未登录", http.MethodGet, "", "", http.StatusUnauthorized},
		{"未知会话", http.MethodGet, "unknown", "", http.StatusUnauthorized},
		{"缺失跨站请求令牌", http.MethodPost, "identity-test", "", http.StatusForbidden},
		{"错误跨站请求令牌", http.MethodPost, "identity-test", "wrong", http.StatusForbidden},
		{"独立登录查询", http.MethodGet, "identity-test", "", http.StatusNoContent},
		{"旧版邀请令牌不可复用", http.MethodPost, "identity-test", auth.HashSecret("csrf:identity-test"), http.StatusForbidden},
		{"合法跨站请求令牌", http.MethodPost, "identity-test", auth.PlatformCSRFToken("identity-test"), http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := platformsession.NewMemoryStore()
			session := platformsession.Session{
				UserUUID: "aa000000-0000-4000-8000-000000000001", VerifiedEmail: "member@example.local",
				OrganizationUUID: "aa000000-0000-4000-8000-000000000002", ExpiresAt: new(time.Now().Add(time.Hour)),
			}
			if err := store.Save(t.Context(), "identity-test", session); err != nil {
				t.Fatal(err)
			}
			server := &Server{platformStore: store}
			called := false
			handler := server.platformIdentityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				email, ok := verifiedInvitationEmail(r)
				if !ok || email != session.VerifiedEmail {
					t.Fatalf("登录身份丢失：%q, %t", email, ok)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(tc.method, "/api/invitations", nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "sessionKey", Value: tc.cookie})
			}
			req.Header.Set("X-CSRF-Token", tc.csrf)
			req.Header.Set("X-Organization-UUID", "removed-organization")
			req.Header.Set("X-Workspace-ID", "archived-workspace")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status || called != (tc.status == http.StatusNoContent) {
				t.Fatalf("status=%d, called=%t, want=%d", response.Code, called, tc.status)
			}
			if tc.status == http.StatusForbidden && len(response.Result().Cookies()) != 0 {
				t.Fatal("权限拒绝不应清除登录会话")
			}
		})
	}
}

func TestPlatformOrganizationHeaderValidation(t *testing.T) {
	for _, tc := range []struct{ name, header, query string }{
		{"非法头", "invalid", ""},
		{"冲突目标", "aa000000-0000-4000-8000-000000000002", "aa000000-0000-4000-8000-000000000003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/bootstrap?organization_uuid="+tc.query, nil)
			request.Header.Set("X-Organization-UUID", tc.header)
			_, err := (&Server{}).resolvePlatformSessionPrincipal(request, platformsession.Session{}, tc.header)
			if err == nil || err.Status != http.StatusBadRequest {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestPlatformOrganizationLog(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		status  int
	}{
		{"仅 Flush", func(w http.ResponseWriter, _ *http.Request) { w.(http.Flusher).Flush() }, http.StatusOK},
		{"显式状态", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			server := &Server{logger: slog.New(slog.NewJSONHandler(&output, nil))}
			principal := auth.Principal{AccountUUID: "actor", OrganizationUUID: "target", WorkspaceUUID: "workspace", CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin, WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
			request := httptest.NewRequest(http.MethodPost, "/v1/agents?secret=private", strings.NewReader("sensitive body"))
			request.Header.Set("Authorization", "secret")
			request.AddCookie(&http.Cookie{Name: "sessionKey", Value: "session-secret"})
			server.servePlatformPrincipal(httptest.NewRecorder(), request, http.HandlerFunc(tc.handler), principal)
			records := parseSlogJSONLines(t, output.String())
			if len(records) != 1 || records[0]["actor_account_uuid"] != "actor" || records[0]["access_source"] != "platform_admin" || records[0]["outcome"] != "success" || records[0]["status"] != float64(tc.status) {
				t.Fatalf("records=%+v", records)
			}
			for _, sensitive := range []string{"secret", "sensitive body", "private"} {
				if strings.Contains(output.String(), sensitive) {
					t.Fatal("日志泄漏敏感值")
				}
			}
		})
	}
}

func TestTargetBootstrapCannotSwallowAuthenticationFailure(t *testing.T) {
	for _, tc := range []struct {
		name, cookie, header string
		status               int
	}{
		{"目标无登录", "", "target", 401},
		{"目标会话失效", "unknown", "target", 401},
		{"身份查询无登录保留匿名合同", "", "", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &Server{platformStore: platformsession.NewMemoryStore()}
			request := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
			request.Header.Set("X-Organization-UUID", tc.header)
			if tc.cookie != "" {
				request.AddCookie(&http.Cookie{Name: "sessionKey", Value: tc.cookie})
			}
			response := httptest.NewRecorder()
			server.optionalPlatformAuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d", response.Code, tc.status)
			}
		})
	}
}
