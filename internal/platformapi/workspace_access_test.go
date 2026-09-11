package platformapi

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

type workspaceRoleStore struct {
	calls int
	roles []db.WorkspaceRoleFact
	err   error
}

func (s *workspaceRoleStore) ListUserWorkspaceRoles(_ context.Context, orgUUID, userUUID string) ([]db.WorkspaceRoleFact, error) {
	s.calls++
	if orgUUID != "org" || userUUID != "user" {
		return nil, errors.New("查询作用域错误")
	}
	return s.roles, s.err
}

func TestConsoleWorkspaceBatchAccess(t *testing.T) {
	workspaces := []ConsoleWorkspace{
		{UUID: "default", OrgUUID: "org", IsDefault: true},
		{UUID: "foreign", OrgUUID: "other"},
		{UUID: "archived", OrgUUID: "org", ArchivedAt: new(time.Now())},
	}
	for i := range 100 {
		workspaces = append(workspaces, ConsoleWorkspace{UUID: fmt.Sprint(i), OrgUUID: "org"})
	}
	for _, tc := range []struct {
		name, orgRole        string
		wantCount, wantCalls int
		fail                 bool
	}{
		{name: "查询失败不返回部分授权", orgRole: "user", wantCalls: 1, fail: true},
		{name: "未知角色不能继承", orgRole: "unknown", wantCalls: 1},
		{name: "普通成员仅见默认和显式空间", orgRole: "user", wantCount: 2, wantCalls: 1},
		{name: "组织管理员无需查询显式角色", orgRole: "admin", wantCount: 102},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &workspaceRoleStore{roles: []db.WorkspaceRoleFact{{WorkspaceUUID: "0", Role: "workspace_admin"}}}
			if tc.fail {
				store.err = errors.New("数据库不可用")
			}
			request := httptest.NewRequest("GET", "/", nil)
			request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{OrganizationUUID: "org", UserUUID: "user", WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: tc.orgRole}}))
			result, err := accessibleConsoleWorkspaces(request, store, workspaces)
			if !errors.Is(err, store.err) || len(result) != tc.wantCount || store.calls != tc.wantCalls {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, store.calls)
			}
			for _, workspace := range result {
				if workspace.UUID == "0" && workspace.EffectiveRole != "workspace_admin" {
					t.Fatalf("显式授权丢失: %+v", workspace)
				}

			}
		})
	}
	t.Run("无用户身份拒绝", func(t *testing.T) {
		_, err := accessibleConsoleWorkspaces(httptest.NewRequest("GET", "/", nil), &workspaceRoleStore{}, workspaces)
		if !errors.Is(err, workspaceaccess.ErrDenied) {
			t.Fatal(err)
		}
	})
}
