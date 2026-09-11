package tests

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func TestWorkspaceAuthorizationInheritance(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("workspace-authorization"))
	defer app.close()
	ctx := t.Context()
	refs := getAdminDefaultIDs(t, app.pool)
	app.seedPlatformSession(t, "workspace-admin-session")
	adminSession, err := app.sessions.Get(ctx, "workspace-admin-session")
	if err != nil {
		t.Fatal(err)
	}
	principal := adminSession.Principal()
	resolver := workspaceaccess.New(app.db)
	workspace := createAdminWorkspace(t, app, "授权测试-"+uniqueAdminSuffix(), nil, nil)
	defaultWorkspace, err := app.db.GetAdminWorkspace(ctx, refs.OrganizationUUID, "default")
	if err != nil {
		t.Fatal(err)
	}
	billingID := seedAdminUser(t, app.pool, "billing-"+uniqueAdminSuffix()+"@example.local", "billing")
	userID := seedAdminUser(t, app.pool, "user-"+uniqueAdminSuffix()+"@example.local", "user")

	t.Run("报表与限流仅允许组织计费角色", func(t *testing.T) {
		paths := []string{"/rate_limits", "/workspaces/" + workspace.ID + "/rate_limits", "/usage_report/messages", "/usage_report/claude_code", "/cost_report"}
		for _, actor := range []struct {
			user   string
			status int
		}{{userID, http.StatusForbidden}, {billingID, http.StatusOK}, {principal.UserExternalID, http.StatusOK}} {
			cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, actor.user)
			for _, path := range paths {
				query := "?starting_at=2026-01-01T00:00:00Z&bucket_width=1d"
				if path == "/usage_report/claude_code" {
					query = "?starting_at=2026-01-01"
				}
				response := app.platformRequest(t, http.MethodGet, "/v1/organizations"+path+query, nil, cookies)
				response.Body.Close()
				if response.StatusCode != actor.status {
					t.Fatalf("%s %s: %d", actor.user, path, response.StatusCode)
				}
			}
		}
	})
	t.Run("普通空间重命名不能使用默认保留名称", func(t *testing.T) {
		cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, principal.UserExternalID)
		validName := "ordinary-renamed-" + uniqueAdminSuffix()
		for _, name := range []string{"default", "DEFAULT", "  DeFaUlT  ", validName} {
			response := app.platformRequest(t, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID, strings.NewReader(`{"name":"`+name+`"}`), cookies)
			response.Body.Close()
			expected := http.StatusBadRequest
			if name == validName {
				expected = http.StatusOK
			}
			if response.StatusCode != expected {
				t.Fatalf("name=%q status=%d", name, response.StatusCode)
			}
		}
	})
	t.Run("普通成员未分配以及组织不匹配拒绝", func(t *testing.T) {
		for _, org := range []string{refs.OrganizationUUID, "bb000000-0000-4000-8000-000000000001"} {
			if _, _, err := resolver.Resolve(ctx, org, userID, workspace.ID); !errors.Is(err, workspaceaccess.ErrDenied) {
				t.Fatalf("err = %v", err)
			}
		}
	})
	t.Run("默认成员保护覆盖所有标识", func(t *testing.T) {
		for _, id := range []string{"default", defaultWorkspace.UUID, defaultWorkspace.ExternalID} {
			_, err := workspaceaccess.ChangeMember(ctx, app.db, principal, id, userID, "workspace_admin", "create")
			if !errors.Is(err, workspaceaccess.ErrDefaultProtected) {
				t.Fatalf("%s: %v", id, err)
			}
		}
	})
	t.Run("默认空间禁止改名归档及同名创建", func(t *testing.T) {
		cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, principal.UserExternalID)
		for _, id := range []string{"default", defaultWorkspace.UUID, defaultWorkspace.ExternalID} {
			for _, operation := range []struct{ suffix, body string }{{"", `{"name":"renamed"}`}, {"/archive", `{}`}} {
				response := app.platformRequest(t, http.MethodPost, "/v1/organizations/workspaces/"+id+operation.suffix, strings.NewReader(operation.body), cookies)
				response.Body.Close()
				if response.StatusCode != http.StatusBadRequest {
					t.Fatalf("默认保护 %s%s: %d", id, operation.suffix, response.StatusCode)
				}
			}
		}
		response := app.platformRequest(t, http.MethodPost, "/api/console/organizations/"+refs.OrganizationUUID+"/workspaces", strings.NewReader(`{"name":"Default"}`), cookies)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("同名创建: %d", response.StatusCode)
		}
	})
	t.Run("未提权计费用户不能自行提权", func(t *testing.T) {
		user, err := app.db.GetAdminUser(ctx, refs.OrganizationUUID, billingID)
		if err != nil {
			t.Fatal(err)
		}
		actor := auth.Principal{OrganizationUUID: refs.OrganizationUUID, UserUUID: user.UUID, UserExternalID: billingID}
		if _, err := workspaceaccess.ChangeMember(ctx, app.db, actor, workspace.ID, billingID, "workspace_admin", "update"); !errors.Is(err, workspaceaccess.ErrDenied) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("计费继承不可删除或降级", func(t *testing.T) {
		for _, operation := range []string{"delete", "update"} {
			_, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, billingID, "workspace_user", operation)
			if !errors.Is(err, workspaceaccess.ErrInheritedRole) {
				t.Fatalf("err = %v", err)
			}
		}
	})
	t.Run("历史默认管理员记录不参与授权且不被改写", func(t *testing.T) {
		user, err := app.db.GetAdminUser(ctx, refs.OrganizationUUID, userID)
		if err != nil {
			t.Fatal(err)
		}
		member, err := app.db.CreateAdminWorkspaceMember(ctx, db.AdminWorkspaceMember{
			ExternalID: "wmem_history_" + uniqueAdminSuffix(), OrganizationUUID: refs.OrganizationUUID,
			WorkspaceUUID: defaultWorkspace.UUID, WorkspaceExternalID: defaultWorkspace.ExternalID,
			UserUUID: user.UUID, UserExternalID: userID, WorkspaceRole: "workspace_admin", CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
		assertEffectiveWorkspaceRole(t, resolver, refs.OrganizationUUID, userID, "default", "workspace_user")
		if err := app.db.Seed(ctx, nil); err != nil {
			t.Fatal(err)
		}
		retained, err := app.db.GetAdminWorkspaceMember(ctx, refs.OrganizationUUID, defaultWorkspace.ExternalID, userID)
		if err != nil || retained != member {
			t.Fatalf("历史成员改变: %+v, %v", retained, err)
		}
	})
	t.Run("计费提权与恢复幂等且无需继承记录", func(t *testing.T) {
		assertEffectiveWorkspaceRole(t, resolver, refs.OrganizationUUID, billingID, workspace.ID, "workspace_billing")
		for _, role := range []string{"workspace_admin", "workspace_admin", "workspace_billing", "workspace_billing"} {
			if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, billingID, role, "update"); err != nil {
				t.Fatal(err)
			}
			assertEffectiveWorkspaceRole(t, resolver, refs.OrganizationUUID, billingID, workspace.ID, role)
		}
		if _, err := app.db.GetAdminWorkspaceMember(ctx, refs.OrganizationUUID, workspace.ID, billingID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("恢复继承后仍有显式成员: %v", err)
		}
	})
	t.Run("组织降级保留显式授权移除后立即拒绝", func(t *testing.T) {
		if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, billingID, "workspace_admin", "update"); err != nil {
			t.Fatal(err)
		}
		if _, err := app.db.UpdateAdminUserRole(ctx, refs.OrganizationUUID, billingID, "developer"); err != nil {
			t.Fatal(err)
		}
		assertEffectiveWorkspaceRole(t, resolver, refs.OrganizationUUID, billingID, workspace.ID, "workspace_admin")
		if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, billingID, "", "delete"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolver.Resolve(ctx, refs.OrganizationUUID, billingID, workspace.ID); !errors.Is(err, workspaceaccess.ErrDenied) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("用户接口不能使用历史默认授权创建资源", func(t *testing.T) {
		cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, userID)
		response := app.platformRequest(t, http.MethodGet, "/v1/files?beta=true", nil, cookies)
		defer response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d", response.StatusCode)
		}
	})
	t.Run("工作区管理员可管理成员但不能管理组织", func(t *testing.T) {
		if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, userID, "workspace_admin", "create"); err != nil {
			t.Fatal(err)
		}
		cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, userID)
		for _, test := range []struct {
			path   string
			status int
		}{
			{"/v1/organizations/users", http.StatusForbidden},
			{"/v1/organizations/workspaces/" + workspace.ID + "/members/user_missing", http.StatusNotFound},
			{"/v1/organizations/workspaces/" + workspace.ID + "/members/" + billingID, http.StatusNotFound},
			{"/v1/organizations/workspaces/" + workspace.ID + "/members/" + userID, http.StatusOK},
			{"/v1/organizations/workspaces/" + workspace.ID + "/members/" + principal.UserExternalID, http.StatusOK},
			{"/v1/organizations/workspaces/" + workspace.ID + "/members", http.StatusOK},
		} {
			response := app.platformRequest(t, http.MethodGet, test.path, nil, cookies)
			response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("%s: status = %d", test.path, response.StatusCode)
			}
		}
		response := app.platformRequest(t, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID+"/members/"+billingID,
			strings.NewReader(`{"workspace_role":"workspace_user"}`), cookies)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("无显式成员更新 status = %d", response.StatusCode)
		}
	})
	t.Run("批量角色排除历史默认成员和跨组织数据", func(t *testing.T) {
		user, err := app.db.GetAdminUser(ctx, refs.OrganizationUUID, userID)
		if err != nil {
			t.Fatal(err)
		}
		ordinary, err := app.db.GetAdminWorkspace(ctx, refs.OrganizationUUID, workspace.ID)
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := app.db.ListUserWorkspaceRoles(ctx, "bb000000-0000-4000-8000-000000000001", user.UUID)
		if err != nil || len(foreign) != 0 {
			t.Fatalf("跨组织角色 = %+v, err = %v", foreign, err)
		}
		facts, err := app.db.ListUserWorkspaceRoles(ctx, refs.OrganizationUUID, user.UUID)
		if err != nil || len(facts) != 1 || facts[0].WorkspaceUUID != ordinary.UUID || facts[0].Role != "workspace_admin" {
			t.Fatalf("角色 = %+v, err = %v", facts, err)
		}
	})
	t.Run("工作区Key独立于创建者但归档拒绝新请求", func(t *testing.T) {
		creator, err := app.db.GetAdminUser(ctx, refs.OrganizationUUID, userID)
		if err != nil {
			t.Fatal(err)
		}
		ordinary, err := app.db.GetAdminWorkspace(ctx, refs.OrganizationUUID, workspace.ID)
		if err != nil {
			t.Fatal(err)
		}
		key, err := app.db.CreateConsoleAPIKey(ctx, platform.CreateConsoleAPIKeyInput{
			OrgUUID: refs.OrganizationUUID, WorkspaceUUID: ordinary.UUID, WorkspaceDisplayID: workspace.ID,
			Name: "权限验收", CreatedByUserUUID: &creator.UUID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.db.DeleteAdminUser(ctx, refs.OrganizationUUID, userID); err != nil {
			t.Fatal(err)
		}
		request := func(expected int) {
			response := app.platformRequestWithHeaders(t, http.MethodGet, "/v1/files?beta=true", nil, nil, map[string]string{"X-Api-Key": key.RawKey, "anthropic-beta": "files-api-2025-04-14"})
			response.Body.Close()
			if response.StatusCode != expected {
				t.Fatalf("status = %d, want %d", response.StatusCode, expected)
			}
		}
		request(http.StatusOK)
		if _, err := app.db.ArchiveAdminWorkspace(ctx, refs.OrganizationUUID, workspace.ID); err != nil {
			t.Fatal(err)
		}
		request(http.StatusForbidden)
		cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, principal.UserExternalID)
		response := app.platformRequest(t, http.MethodGet, "/api/console/organizations/"+refs.OrganizationUUID+"/workspaces/"+workspace.ID+"/api_keys", nil, cookies)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("归档空间路径直访: %d", response.StatusCode)
		}
		if _, _, err := resolver.Resolve(ctx, refs.OrganizationUUID, principal.UserExternalID, workspace.ID); !errors.Is(err, workspaceaccess.ErrDenied) {
			t.Fatalf("归档用户访问: %v", err)
		}
	})
}

func assertEffectiveWorkspaceRole(t *testing.T, resolver *workspaceaccess.Service, org, user, workspace, expected string) {
	t.Helper()
	_, access, err := resolver.Resolve(t.Context(), org, user, workspace)
	if err != nil || access.Role != expected {
		t.Fatalf("role = %s, expected = %s, err = %v", access.Role, expected, err)
	}
}

func workspaceUserCookies(t *testing.T, app *testApp, orgUUID, userID string) []*http.Cookie {
	t.Helper()
	user, err := app.db.GetAdminUser(t.Context(), orgUUID, userID)
	if err != nil {
		t.Fatal(err)
	}
	key := "test-session-" + uniqueAdminSuffix()
	session, err := app.db.ResolvePlatformSessionIdentity(context.Background(), platformsession.CreateInput{SessionKey: key, OrgUUID: orgUUID, UserUUID: user.UUID})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.sessions.Save(t.Context(), key, session); err != nil {
		t.Fatal(err)
	}
	return []*http.Cookie{{Name: "sessionKey", Value: key}, {Name: "lastActiveOrg", Value: orgUUID}}
}

func TestWorkspaceMemberMutationObservesCommittedRevocation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("workspace-concurrent-revocation"))
	defer app.close()
	ctx := t.Context()
	refs := getAdminDefaultIDs(t, app.pool)
	workspace := createAdminWorkspace(t, app, "并发撤权-"+uniqueAdminSuffix(), nil, nil)
	actorID := seedAdminUser(t, app.pool, "actor-"+uniqueAdminSuffix()+"@example.local", "admin")
	targetID := seedAdminUser(t, app.pool, "target-"+uniqueAdminSuffix()+"@example.local", "user")
	actor, err := app.db.GetAdminUser(ctx, refs.OrganizationUUID, actorID)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := app.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback(context.Background())
	if _, err := transaction.Exec(ctx, `UPDATE users SET role='user' WHERE organization_uuid=$1 AND uuid=$2`, refs.OrganizationUUID, actor.UUID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := workspaceaccess.ChangeMember(ctx, app.db, auth.Principal{OrganizationUUID: refs.OrganizationUUID, UserExternalID: actorID, UserUUID: actor.UUID}, workspace.ID, targetID, "workspace_admin", "create")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("成员变更没有等待组织身份事务: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, workspaceaccess.ErrDenied) {
			t.Fatalf("提交撤权后变更结果: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("成员变更事务未结束")
	}
	if _, err := app.db.GetAdminWorkspaceMember(ctx, refs.OrganizationUUID, workspace.ID, targetID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("拒绝变更产生了授权: %v", err)
	}
}

func TestWorkspaceMemoryReadsRejectOtherWorkspace(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("workspace-memory-isolation"))
	defer app.close()
	refs := getAdminDefaultIDs(t, app.pool)
	store := createMemoryStore(t, app, "隔离验收")
	memory := createMemory(t, app, store.ID, "/check.md", "只属于当前工作区")
	workspace := createAdminWorkspace(t, app, "记忆隔离-"+uniqueAdminSuffix(), nil, nil)
	userID := seedAdminUser(t, app.pool, "memory-"+uniqueAdminSuffix()+"@example.local", "admin")
	cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, userID)
	base := "/v1/memory_stores/" + store.ID
	paths := []string{base, base + "/memories", base + "/memories?depth=1", base + "/memories/" + memory.ID, base + "/memory_versions", base + "/memory_versions/" + memory.MemoryVersionID}
	for _, scenario := range []struct {
		name, workspaceID string
		status            int
	}{
		{"跨工作区读取拒绝", workspace.ID, http.StatusNotFound},
		{"所属工作区读取正常", "default", http.StatusOK},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, path := range paths {
				separator := "?"
				if strings.Contains(path, "?") {
					separator = "&"
				}
				response := app.platformRequestWithHeaders(t, http.MethodGet, path+separator+"beta=true", nil, cookies, map[string]string{"X-Workspace-ID": scenario.workspaceID, "anthropic-beta": "managed-agents-2026-04-01"})
				response.Body.Close()
				if response.StatusCode != scenario.status {
					t.Fatalf("%s: %d, want %d", path, response.StatusCode, scenario.status)
				}
			}
		})
	}
}

func TestArchivedWorkspaceAdminRequests(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("archived-workspace-admin"))
	defer app.close()
	ctx := t.Context()
	refs := getAdminDefaultIDs(t, app.pool)
	app.seedPlatformSession(t, "archived-workspace-admin-session")
	session, err := app.sessions.Get(ctx, "archived-workspace-admin-session")
	if err != nil {
		t.Fatal(err)
	}
	cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, session.Principal().UserExternalID)
	workspace := createAdminWorkspace(t, app, "归档管理测试-"+uniqueAdminSuffix(), nil, nil)
	archived, err := app.db.ArchiveAdminWorkspace(ctx, refs.OrganizationUUID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, method, suffix, body string }{
		{"改名", http.MethodPost, "", `{"name":"renamed-` + uniqueAdminSuffix() + `"}`},
		{"修改标签", http.MethodPost, "", `{"tags":{"team":"archived"}}`},
		{"修改数据驻留", http.MethodPost, "", `{"data_residency":{"allowed_inference_geos":"unrestricted","default_inference_geo":"us"}}`},
		{"成员列表", http.MethodGet, "/members", ""},
		{"单成员读取", http.MethodGet, "/members/" + session.Principal().UserExternalID, ""},
	}
	for _, id := range []string{archived.ExternalID, archived.UUID} {
		for _, tc := range cases {
			t.Run(id+"/"+tc.name, func(t *testing.T) {
				response := app.platformRequest(t, tc.method, "/v1/organizations/workspaces/"+id+tc.suffix, strings.NewReader(tc.body), cookies)
				defer response.Body.Close()
				if response.StatusCode != http.StatusForbidden {
					t.Errorf("归档空间请求状态 = %d，期望 403", response.StatusCode)
				}
			})
		}
	}
	current, err := app.db.GetAdminWorkspace(ctx, refs.OrganizationUUID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, archived) {
		t.Error("拒绝请求后归档空间记录发生变化")
	}
	active := createAdminWorkspace(t, app, "有效管理测试-"+uniqueAdminSuffix(), nil, nil)
	for _, tc := range cases {
		t.Run("有效空间/"+tc.name, func(t *testing.T) {
			response := app.platformRequest(t, tc.method, "/v1/organizations/workspaces/"+active.ID+tc.suffix, strings.NewReader(tc.body), cookies)
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Errorf("有效空间请求状态 = %d，期望 200", response.StatusCode)
			}
		})
	}
}
