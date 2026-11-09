package platformapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestBootstrapPlatformAccessBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		withSession bool
		lookupErr   error
		want        int
	}{
		{name: "查询失败不得返回成功", withSession: true, lookupErr: errors.New("database unavailable"), want: http.StatusServiceUnavailable},
		{name: "无会话不查询全局授权", want: http.StatusOK},
		{name: "全局授权与组织权限独立", withSession: true, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			handler := handleBootstrap(&sessionBootstrapStore{}, func(_ context.Context, session platformsession.Session) (PlatformAccess, error) {
				calls++
				if session.UserUUID != "stable-login" {
					t.Fatalf("收到组织成员而非登录身份：%q", session.UserUUID)
				}
				return PlatformAccess{Role: new("super_admin"), Permissions: []string{"platform:access"}}, tc.lookupErr
			})
			workspaceAccess := auth.WorkspaceAccess{OrganizationRole: "user", Role: "workspace_developer"}
			ctx := auth.WithPrincipal(t.Context(), auth.Principal{UserUUID: "org-member", WorkspaceAccess: workspaceAccess})
			if tc.withSession {
				ctx = platformsession.WithSession(ctx, platformsession.Session{UserUUID: "stable-login", VerifiedEmail: "admin@example.com"})
			}
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil).WithContext(ctx))
			if w.Code != tc.want {
				t.Fatalf("状态 = %d，预期 %d：%s", w.Code, tc.want, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("授权响应（含故障响应）不得缓存")
			}
			if !tc.withSession && calls != 0 {
				t.Fatal("无会话触发了授权查询")
			}
			if tc.lookupErr != nil {
				return
			}
			var body BootstrapCompatibilityResponse
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(body.CurrentUserAccess.Permissions, workspaceAccess.Permissions()) {
				t.Fatal("全局权限污染了组织权限")
			}
			if tc.withSession {
				if calls != 1 || body.PlatformAccess.Role == nil || *body.PlatformAccess.Role != "super_admin" || !slices.Equal(body.PlatformAccess.Permissions, []string{"platform:access"}) {
					t.Fatalf("全局授权映射错误：%+v", body.PlatformAccess)
				}
				if !slices.Equal(body.Account.Permissions, workspaceAccess.Permissions()) {
					t.Fatal("全局权限污染了账号组织权限")
				}
			} else if body.PlatformAccess.Role != nil || body.PlatformAccess.Permissions == nil || len(body.PlatformAccess.Permissions) != 0 {
				t.Fatalf("无授权必须返回 null 角色和空权限数组：%+v", body.PlatformAccess)
			}
		})
	}
}

func TestBootstrapSessionAndCurrentPermissions(t *testing.T) {
	store := &sessionBootstrapStore{}
	handler := handleBootstrap(store, nil)
	for _, withSession := range []bool{false, true} {
		session := platformsession.Session{UserUUID: "stable-login", VerifiedEmail: "verified@example.com"}
		access := auth.WorkspaceAccess{OrganizationRole: "user", Role: "workspace_developer"}
		ctx := auth.WithPrincipal(t.Context(), auth.Principal{UserUUID: "current-org-member", WorkspaceAccess: access})
		if withSession {
			ctx = platformsession.WithSession(ctx, session)
		}
		request := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil).WithContext(ctx)
		request.AddCookie(&http.Cookie{Name: "sessionKey", Value: "trusted-session-key"})
		response := httptest.NewRecorder()
		handler(response, request)
		var body BootstrapCompatibilityResponse
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK {
			t.Fatalf("状态码: %d", response.Code)
		}
		if !withSession {
			if body.Account != nil || body.CSRFToken != "" {
				t.Fatal("只有 Principal 不能构建登录账号")
			}
			continue
		}
		if body.Account == nil || body.Account.UUID != session.UserUUID || len(body.Account.Memberships) != 0 ||
			body.CSRFToken != auth.PlatformCSRFToken("trusted-session-key") ||
			!slices.Equal(body.Account.Permissions, access.Permissions()) ||
			!slices.Equal(body.CurrentUserAccess.Permissions, access.Permissions()) {
			t.Fatalf("会话与权限映射错误: %+v", body)
		}
	}
}

type sessionBootstrapStore struct {
	orgs  []UserOrganizationRecord
	email string
}

func (s *sessionBootstrapStore) GetBootstrapUser(_ context.Context, id string) (*UserRecord, error) {
	return &UserRecord{UUID: id, Email: "数据库显示邮箱"}, nil
}

func (s *sessionBootstrapStore) ListBootstrapOrganizationsByEmail(_ context.Context, email string) ([]UserOrganizationRecord, error) {
	s.email = email
	return s.orgs, nil
}

func TestBootstrapStableAccount(t *testing.T) {
	session := platformsession.Session{UserUUID: "login-user", VerifiedEmail: "verified@example.com",
		OrganizationUUID: "source", HomeOrganizationUUID: "home"}
	for _, tc := range []struct {
		name   string
		orgIDs []string
		want   string
	}{
		{"无组织仍已登录", nil, ""},
		{"注册组织优先", []string{"first", "source", "home"}, "home"},
		{"来源组织回退", []string{"first", "source"}, "source"},
		{"首个成员回退", []string{"first"}, "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &sessionBootstrapStore{}
			for _, id := range tc.orgIDs {
				store.orgs = append(store.orgs, UserOrganizationRecord{
					OrganizationRecord: OrganizationRecord{UUID: id},
					UserUUID:           "member-" + id, UserExternalID: "user_" + id, Role: "user",
				})
			}
			account, selected, err := buildBootstrapAccount(t.Context(), store, session)
			if err != nil || account.UUID != session.UserUUID || selected != tc.want ||
				account.DefaultOrganizationUUID != tc.want || account.Memberships == nil ||
				store.email != session.VerifiedEmail || account.EmailAddress != session.VerifiedEmail {
				t.Fatalf("bootstrap: %+v, %q, %v", account, selected, err)
			}
			for i, membership := range account.Memberships {
				if membership.UserUUID != store.orgs[i].UserUUID || membership.UserID != store.orgs[i].UserExternalID {
					t.Fatalf("组织用户映射: %+v", membership)
				}
			}
		})
	}
}

func (s *sessionBootstrapStore) GetPlatformOrganization(_ context.Context, id string) (*OrganizationRecord, error) {
	return &OrganizationRecord{UUID: id, Name: "目标组织"}, nil
}

func TestBootstrapPlatformOrganizationWithoutMembership(t *testing.T) {
	store := &sessionBootstrapStore{orgs: []UserOrganizationRecord{{OrganizationRecord: OrganizationRecord{UUID: "home"}, UserUUID: "member", Role: "user"}}}
	principal := auth.Principal{AccountUUID: "stable-login", OrganizationUUID: "target", CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin, WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
	ctx := platformsession.WithSession(auth.WithPrincipal(t.Context(), principal), platformsession.Session{UserUUID: "stable-login", VerifiedEmail: "verified@example.com", HomeOrganizationUUID: "home"})
	response := httptest.NewRecorder()
	handleBootstrap(store, nil)(response, httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil).WithContext(ctx))
	var body BootstrapCompatibilityResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || body.OrganizationAccess == nil || body.OrganizationAccess.Organization.UUID != "target" || body.OrganizationAccess.Source != auth.OrganizationAccessPlatformAdmin || body.Account.UUID != "stable-login" || body.Account.DefaultOrganizationUUID != "home" || len(body.Account.Memberships) != 1 || !slices.Contains(body.Account.Permissions, "organization:manage") {
		t.Fatalf("body=%+v", body)
	}
}
