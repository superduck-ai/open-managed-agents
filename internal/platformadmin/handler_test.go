package platformadmin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestAdministrationAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name       string
		method     string
		credential string
		loadErr    error
		verifyErr  error
		unverified bool
		disabled   bool
		csrf       bool
		want       int
	}{
		{name: "无会话", loadErr: ErrUnauthenticated, want: 401},
		{name: "旧会话", unverified: true, want: 403},
		{name: "开发模式", disabled: true, want: 403},
		{name: "普通账号", verifyErr: ErrDenied, want: 403},
		{name: "混用 API key", credential: "X-Api-Key", want: 403},
		{name: "混用 Bearer", credential: "Authorization", want: 403},
		{name: "写入缺少 CSRF", method: http.MethodPost, want: 403},
		{name: "授权查询失败", verifyErr: ErrUnavailable, want: 503},
		{name: "独立于组织上下文", want: 204},
		{name: "合法写入", method: http.MethodPost, csrf: true, want: 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := platformsession.Session{UserUUID: "stable-account", VerifiedEmail: "admin@example.com", AuthenticationMethod: "email_code_v1"}
			if tc.unverified {
				session.AuthenticationMethod = ""
			}
			h := NewHandler(New(nil, nil), func(*http.Request) (platformsession.Session, error) { return session, tc.loadErr }, !tc.disabled)
			h.verify = func(_ context.Context, account string) (db.PlatformAdminAccount, error) {
				if account != session.UserUUID {
					t.Fatalf("授权使用的账号 = %q", account)
				}
				return db.PlatformAdminAccount{UUID: account}, tc.verifyErr
			}
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, "/api/platform-admin/bootstrap", nil)
			r.AddCookie(&http.Cookie{Name: "sessionKey", Value: "test-session"})
			if tc.credential != "" {
				r.Header.Set(tc.credential, "forbidden-credential")
			}
			if tc.csrf {
				r.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken("test-session"))
			}
			w := httptest.NewRecorder()
			h.authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if actorFromRequest(r).AccountUUID != session.UserUUID {
					t.Fatal("请求丢失账号身份")
				}
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("状态 = %d，预期 %d：%s", w.Code, tc.want, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("后台响应必须禁用缓存")
			}
		})
	}
}

func TestAdministrationRevocationIsCheckedOnEveryRequest(t *testing.T) {
	h := NewHandler(New(nil, nil), func(*http.Request) (platformsession.Session, error) {
		return platformsession.Session{UserUUID: "stable-account", VerifiedEmail: "admin@example.com", AuthenticationMethod: "email_code_v1"}, nil
	}, true)
	granted := true
	h.verify = func(context.Context, string) (db.PlatformAdminAccount, error) {
		if !granted {
			return db.PlatformAdminAccount{}, ErrDenied
		}
		return db.PlatformAdminAccount{UUID: "stable-account"}, nil
	}
	routes := h.Routes()
	for _, want := range []int{http.StatusOK, http.StatusForbidden} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/bootstrap", nil)
		r.AddCookie(&http.Cookie{Name: "sessionKey", Value: "test-session"})
		routes.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("状态 = %d，预期 %d：%s", w.Code, want, w.Body.String())
		}
		granted = false
	}
}

func TestAdministrationAuditIsNotAvailable(t *testing.T) {
	h := NewHandler(New(nil, nil), func(*http.Request) (platformsession.Session, error) {
		return platformsession.Session{UserUUID: "stable-account", VerifiedEmail: "admin@example.com", AuthenticationMethod: "email_code_v1"}, nil
	}, true)
	h.verify = func(context.Context, string) (db.PlatformAdminAccount, error) {
		return db.PlatformAdminAccount{UUID: "stable-account"}, nil
	}
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit-events", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("未发布的审计接口状态 = %d，预期 404", w.Code)
	}
	for _, permission := range Permissions(true).Permissions {
		if permission == "platform:audit:read" {
			t.Fatal("当前版本不应返回审计权限")
		}
	}
}
