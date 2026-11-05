package platformapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type apiKeyCreatorStore struct {
	consoleAPIKeyScopeStore
	accountErr error
	memberErr  error
}

func (s *apiKeyCreatorStore) GetPlatformAdminAccount(_ context.Context, accountUUID string) (db.PlatformAdminAccount, error) {
	if accountUUID != "account" {
		return db.PlatformAdminAccount{}, errors.New("账号范围不匹配")
	}
	return db.PlatformAdminAccount{Email: "actor@example.com"}, s.accountErr
}

func (s *apiKeyCreatorStore) GetActivePlatformUserByEmail(_ context.Context, organizationUUID, email string) (db.AdminUser, error) {
	if organizationUUID != "00000000-0000-4000-8000-000000000002" || email != "actor@example.com" {
		return db.AdminUser{}, errors.New("组织成员范围不匹配")
	}
	return db.AdminUser{UUID: "member"}, s.memberErr
}

func TestPlatformAPIKeyCreatorUsesExistingOrganizationMembership(t *testing.T) {
	for _, tc := range []struct {
		name       string
		accountErr error
		memberErr  error
		status     int
		creator    string
	}{
		{"账号查询失败", errors.New("数据库不可用"), nil, http.StatusInternalServerError, ""},
		{"成员查询失败", nil, errors.New("数据库不可用"), http.StatusInternalServerError, ""},
		{"无组织成员时不冒用管理员账号", nil, db.ErrNotFound, http.StatusOK, ""},
		{"已有组织成员时保存成员身份", nil, nil, http.StatusOK, "member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := consoleWorkspaceAPIKeyTestRequest("workspace_test")
			request.Method = http.MethodPost
			request.Body = io.NopCloser(strings.NewReader(`{"name":"platform key"}`))
			principal := auth.Principal{
				AccountUUID: "account", OrganizationUUID: "00000000-0000-4000-8000-000000000002",
				CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin,
				WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"},
			}
			store := &apiKeyCreatorStore{
				consoleAPIKeyScopeStore: consoleAPIKeyScopeStore{workspaces: []ConsoleWorkspace{{UUID: "00000000-0000-4000-8000-000000000001", ExternalID: "workspace_test"}}},
				accountErr:              tc.accountErr, memberErr: tc.memberErr,
			}
			response := httptest.NewRecorder()
			handleCreateConsoleWorkspaceAPIKey(store).ServeHTTP(response, request.WithContext(auth.WithPrincipal(request.Context(), principal)))
			if response.Code != tc.status {
				t.Fatalf("状态码 = %d，预期 %d：%s", response.Code, tc.status, response.Body.String())
			}
			if tc.status != http.StatusOK {
				if store.createInput != nil {
					t.Fatal("身份解析失败后不应创建 key")
				}
				return
			}
			creator := store.createInput.CreatedByUserUUID
			if store.createInput.RuntimeUserUUID != "account" {
				t.Fatalf("稳定运行账号丢失：%+v", store.createInput)
			}
			if tc.creator == "" && creator != nil || tc.creator != "" && (creator == nil || *creator != tc.creator) {
				t.Fatalf("创建者 = %v，预期 %q", creator, tc.creator)
			}
		})
	}
}
