//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func TestConsoleDefaultWorkspaceMemberPermissions(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("console-default-workspace-members"))
	defer app.close()
	refs := getAdminDefaultIDs(t, app.pool)
	actor := seedAdminUser(t, app.pool, "default-manager-"+uniqueAdminSuffix()+"@example.local", "admin")
	target := seedAdminUser(t, app.pool, "default-target-"+uniqueAdminSuffix()+"@example.local", "user")
	cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, actor)
	path := "/api/console/organizations/" + refs.OrganizationUUID + "/workspaces/workspace_default"
	t.Run("directory is read only", func(t *testing.T) {
		response := app.platformRequest(t, http.MethodGet, path+"/members", nil, cookies)
		defer response.Body.Close()
		var directory struct {
			IsDefault        bool `json:"is_default"`
			CanManageMembers bool `json:"can_manage_members"`
			Members          []struct {
				CanEdit   bool `json:"can_edit"`
				CanRemove bool `json:"can_remove"`
			} `json:"members"`
		}
		if err := json.UnmarshalRead(response.Body, &directory); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || !directory.IsDefault || directory.CanManageMembers || len(directory.Members) < 2 {
			t.Fatalf("unexpected Default directory: status=%d directory=%+v", response.StatusCode, directory)
		}
		for _, member := range directory.Members {
			if member.CanEdit || member.CanRemove {
				t.Fatalf("Default member is editable: %+v", member)
			}
		}
	})
	for _, tc := range []struct{ method, suffix, body string }{
		{http.MethodGet, "/member-candidates", ""},
		{http.MethodPost, "/members", `{"user_id":"` + target + `","workspace_role":"workspace_user"}`},
		{http.MethodPost, "/members/" + target, `{"workspace_role":"workspace_admin"}`},
		{http.MethodDelete, "/members/" + target, ""},
	} {
		t.Run(tc.method+tc.suffix, func(t *testing.T) {
			response := app.platformRequest(t, tc.method, path+tc.suffix, strings.NewReader(tc.body), cookies)
			defer response.Body.Close()
			var result struct {
				Error string `json:"error"`
			}
			body := readAll(t, response.Body)
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatalf("decode Default error: %v: %s", err, body)
			}
			if response.StatusCode != http.StatusConflict || result.Error != "default_workspace_protected" {
				t.Fatalf("Default operation accepted: status=%d body=%s", response.StatusCode, body)
			}
		})
	}
}

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
	target := seedAdminUser(t, app.pool, "workspace-target-"+uniqueAdminSuffix()+"@example.local", "user")
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
		{http.MethodPost, path + "/members/" + target, `{"workspace_role":"workspace_user"}`, http.StatusOK},
		{http.MethodDelete, path + "/members/" + target, "", http.StatusOK},
	} {
		response := app.platformRequest(t, tc.method, tc.path, strings.NewReader(tc.body), cookies)
		body := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("%s %s: %d, 预期 %d: %s", tc.method, tc.path, response.StatusCode, tc.status, body)
		}
		if strings.HasSuffix(tc.path, "/member-candidates") && !strings.Contains(string(body), consoleTaggedUserID(targetUser.UUID)) {
			t.Fatal("普通账号未出现在可添加候选中")
		}
	}
	if _, err := workspaceaccess.ChangeMember(ctx, app.db, session.Principal(), workspace.ID, actor, "workspace_user", "update"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, suffix, body string }{
		{http.MethodGet, "/members", ""},
		{http.MethodGet, "/member-candidates", ""},
	} {
		response := app.platformRequest(t, tc.method, path+tc.suffix, strings.NewReader(tc.body), cookies)
		body := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("降级后仍可管理成员: %d %s", response.StatusCode, body)
		}
	}
	for _, member := range []struct{ name, reference string }{
		{"existing external ID", target},
		{"existing Console ID", consoleTaggedUserID(targetUser.UUID)},
		{"existing UUID", targetUser.UUID},
		{"missing member", "user_000000000000000000000000"},
	} {
		for _, operation := range []struct{ name, method, suffix, body string }{
			{"create", http.MethodPost, "/members", `{"user_id":"` + member.reference + `","workspace_role":"workspace_user"}`},
			{"update", http.MethodPost, "/members/" + member.reference, `{"workspace_role":"workspace_admin"}`},
			{"delete", http.MethodDelete, "/members/" + member.reference, ""},
		} {
			t.Run(member.name+"/"+operation.name, func(t *testing.T) {
				response := app.platformRequest(t, operation.method, path+operation.suffix, strings.NewReader(operation.body), cookies)
				defer response.Body.Close()
				var result struct {
					Error   string `json:"error"`
					Message string `json:"message"`
				}
				if err := json.UnmarshalRead(response.Body, &result); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != http.StatusForbidden || result.Error != "workspace_access_denied" || result.Message != "You do not have permission to manage this workspace." {
					t.Fatalf("普通成员写入响应泄露目标状态: status=%d result=%+v", response.StatusCode, result)
				}
			})
		}
	}
}
