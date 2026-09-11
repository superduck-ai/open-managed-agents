package platformapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestBootstrapSessionAndCurrentPermissions(t *testing.T) {
	store := &sessionBootstrapStore{}
	handler := handleBootstrap(store)
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
			body.CSRFToken != platformsession.CSRFToken("trusted-session-key") ||
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
