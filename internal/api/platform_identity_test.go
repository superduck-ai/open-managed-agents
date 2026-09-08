package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		{"合法跨站请求令牌", http.MethodPost, "identity-test", platformsession.CSRFToken("identity-test"), http.StatusNoContent},
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
			// 错误的旧上下文不能阻断邀请入口，也不能改变已验证邮箱。
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
