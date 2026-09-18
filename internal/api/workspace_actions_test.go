package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

// 组织成员权限不能挡住工作区成员请求，后者由资源层检查目标空间权限。
func TestPlatformWorkspaceMemberRoutesUseTargetAuthorization(t *testing.T) {
	access := auth.WorkspaceAccess{OrganizationRole: "user", Role: "workspace_user", Source: "organization"}
	for _, path := range []string{
		"/api/console/organizations/org/members",
		"/api/console/organizations/org/invites",
	} {
		if platformActionAllowed(httptest.NewRequest(http.MethodGet, path, nil), access) {
			t.Fatalf("普通成员获得组织权限: %s", path)
		}
	}
	for _, path := range []string{
		"/api/console/organizations/org/workspaces/wrk/members",
		"/api/console/organizations/org/workspaces/wrk/members/user",
		"/api/console/organizations/org/workspaces/wrk/member-candidates",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			if !platformActionAllowed(httptest.NewRequest(method, path, nil), access) {
				t.Fatalf("未交由目标工作区鉴权: %s %s", method, path)
			}
		}
	}
}
