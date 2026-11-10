package platformapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type apiKeyCreatorStore struct {
	consoleAPIKeyScopeStore
	accountErr   error
	memberErr    error
	accountCalls int
	memberCalls  int
}

func (s *apiKeyCreatorStore) GetPlatformAdminAccount(_ context.Context, accountUUID string) (db.PlatformAdminAccount, error) {
	s.accountCalls++
	if accountUUID != "account" {
		return db.PlatformAdminAccount{}, errors.New("账号范围不匹配")
	}
	return db.PlatformAdminAccount{Email: "actor@example.com"}, s.accountErr
}

func (s *apiKeyCreatorStore) GetActivePlatformUserByEmail(_ context.Context, organizationUUID, email string) (db.AdminUser, error) {
	s.memberCalls++
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
			handleCreateConsoleWorkspaceAPIKey(store, testLogger()).ServeHTTP(response, request.WithContext(auth.WithPrincipal(request.Context(), principal)))
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

func TestPlatformAPIKeyCreatorLogsResolutionFailure(t *testing.T) {
	const (
		organizationUUID = "00000000-0000-4000-8000-000000000002"
		workspaceUUID    = "00000000-0000-4000-8000-000000000001"
	)
	request := consoleWorkspaceAPIKeyTestRequest("workspace_test")
	request.Method = http.MethodPost
	request.Body = io.NopCloser(strings.NewReader(`{"name":"platform key"}`))
	principal := auth.Principal{
		AccountUUID: "account", OrganizationUUID: organizationUUID,
		CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin,
		WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"},
	}
	store := &apiKeyCreatorStore{
		consoleAPIKeyScopeStore: consoleAPIKeyScopeStore{workspaces: []ConsoleWorkspace{{UUID: workspaceUUID, ExternalID: "workspace_test"}}},
		accountErr:              errors.New("账号数据库不可用"),
	}
	var logs bytes.Buffer
	response := httptest.NewRecorder()
	handleCreateConsoleWorkspaceAPIKey(store, slog.New(slog.NewJSONHandler(&logs, nil))).ServeHTTP(
		response,
		request.WithContext(auth.WithPrincipal(request.Context(), principal)),
	)
	if response.Code != http.StatusInternalServerError || store.createInput != nil {
		t.Fatalf("status=%d creator=%+v", response.Code, store.createInput)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("解析日志 %q：%v", logs.String(), err)
	}
	if record["level"] != slog.LevelError.String() || record["msg"] != "failed to resolve api key creator" ||
		record["error"] != "账号数据库不可用" || record["organization_id"] != organizationUUID ||
		record["workspace_id"] != workspaceUUID {
		t.Fatalf("错误日志不完整：%+v", record)
	}
}

func TestConsoleAPIKeyCreatorSkipsResolverOutsidePlatformAccess(t *testing.T) {
	for _, tc := range []struct{ name, user, want string }{
		{"非平台来源且没有成员身份", "", ""},
		{"真实成员使用自身身份", "member", "member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &apiKeyCreatorStore{}
			creator, err := ResolveConsoleAPIKeyCreator(t.Context(), store, auth.Principal{AccountUUID: "account", UserUUID: tc.user, OrganizationUUID: "00000000-0000-4000-8000-000000000002"})
			if err != nil || store.accountCalls != 0 || store.memberCalls != 0 {
				t.Fatalf("不应解析账号或成员：账号=%d，成员=%d，错误=%v", store.accountCalls, store.memberCalls, err)
			}
			if tc.want == "" && creator != nil || tc.want != "" && (creator == nil || *creator != tc.want) {
				t.Fatalf("创建者错误：%v", creator)
			}
		})
	}
}
