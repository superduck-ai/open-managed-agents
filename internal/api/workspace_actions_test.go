package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestPlatformWorkspaceMemberRoutesUseTargetAuthorization(t *testing.T) {
	access := auth.WorkspaceAccess{OrganizationRole: "user", Role: "workspace_user", Source: "organization"}
	for _, path := range []string{
		"/api/console/organizations/org/invites",
	} {
		if platformActionAllowed(httptest.NewRequest(http.MethodGet, path, nil), access) {
			t.Fatalf("普通成员获得组织权限: %s", path)
		}
	}
	if !platformActionAllowed(httptest.NewRequest(http.MethodGet, "/api/console/organizations/org/members", nil), access) {
		t.Fatal("用户无法查看组织成员")
	}
	if platformActionAllowed(httptest.NewRequest(http.MethodPost, "/api/console/organizations/org/members/user", nil), access) {
		t.Fatal("用户获得组织成员写权限")
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
