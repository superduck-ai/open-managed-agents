package tests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func TestConsoleWorkspaceMemberTargetAuthorization(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("console-workspace-members"))
	defer app.close()
	ctx := t.Context()
	refs := getAdminDefaultIDs(t, app.pool)
	app.seedPlatformSession(t, "console-workspace-member-owner")
	session, err := app.sessions.Get(ctx, "console-workspace-member-owner")
	if err != nil {
		t.Fatal(err)
	}
	workspace := createAdminWorkspace(t, app, "成员权限-"+uniqueAdminSuffix(), nil, nil)
	other := createAdminWorkspace(t, app, "其他空间-"+uniqueAdminSuffix(), nil, nil)
	actor := seedAdminUser(t, app.pool, "workspace-manager-"+uniqueAdminSuffix()+"@example.local", "user")
	target := seedAdminUser(t, app.pool, "workspace-target-"+uniqueAdminSuffix()+"@example.local", "billing")
	if _, err := workspaceaccess.ChangeMember(ctx, app.db, session.Principal(), workspace.ID, actor, "workspace_admin", "create"); err != nil {
		t.Fatal(err)
	}
	targetUser, err := app.db.GetAdminUser(ctx, refs.OrganizationUUID, target)
	if err != nil {
		t.Fatal(err)
	}
	cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, actor)
	prefix := "/api/console/organizations/" + refs.OrganizationUUID
	path := prefix + "/workspaces/" + workspace.ID
	// 会话仍指向 Default，必须按 URL 中目标工作区的最新角色授权。
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, prefix + "/members", "", http.StatusForbidden},
		{http.MethodGet, prefix + "/workspaces/" + other.ID + "/members", "", http.StatusForbidden},
		{http.MethodPost, prefix + "/workspaces/" + other.ID + "/members", `{"user_id":"` + target + `","workspace_role":"workspace_user"}`, http.StatusForbidden},
		{http.MethodGet, path + "/members", "", http.StatusOK},
		{http.MethodGet, path + "/member-candidates", "", http.StatusOK},
		{http.MethodPost, path + "/members", `{"user_id":"` + target + `","workspace_role":"workspace_user"}`, http.StatusOK},
		{http.MethodPost, path + "/members/" + target, `{"workspace_role":"workspace_developer"}`, http.StatusOK},
		{http.MethodDelete, path + "/members/" + target, "", http.StatusOK},
	} {
		response := app.platformRequest(t, tc.method, tc.path, strings.NewReader(tc.body), cookies)
		body := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("%s %s: %d, 预期 %d: %s", tc.method, tc.path, response.StatusCode, tc.status, body)
		}
		if strings.HasSuffix(tc.path, "/member-candidates") && !strings.Contains(string(body), consoleTaggedUserID(targetUser.UUID)) {
			t.Fatal("Billing 账号未出现在可添加候选中")
		}
	}
	if _, err := workspaceaccess.ChangeMember(ctx, app.db, session.Principal(), workspace.ID, actor, "workspace_user", "update"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, suffix, body string }{
		{http.MethodGet, "/member-candidates", ""},
		{http.MethodPost, "/members", `{"user_id":"` + target + `","workspace_role":"workspace_user"}`},
	} {
		response := app.platformRequest(t, tc.method, path+tc.suffix, strings.NewReader(tc.body), cookies)
		body := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("降级后仍可管理成员: %d %s", response.StatusCode, body)
		}
	}
}
