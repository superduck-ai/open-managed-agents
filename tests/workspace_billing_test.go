package tests

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func TestBillingWorkspaceInheritance(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("billing-inheritance"))
	defer app.close()
	ctx := t.Context()
	refs := getAdminDefaultIDs(t, app.pool)
	app.seedPlatformSession(t, "billing-inheritance-admin")
	session, err := app.sessions.Get(ctx, "billing-inheritance-admin")
	if err != nil {
		t.Fatal(err)
	}
	principal := session.Principal()
	resolver := workspaceaccess.New(app.db)
	workspace := createAdminWorkspace(t, app, "计费专项-"+uniqueAdminSuffix(), nil, nil)
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
	t.Run("计费可用Workbench但不能开发或管理组织", func(t *testing.T) {
		cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, billingID)
		for _, tc := range []struct {
			path   string
			status int
		}{
			{"/v1/files?beta=true", http.StatusForbidden},
			{"/v1/organizations/users", http.StatusForbidden},
			{"/api/organizations/" + refs.OrganizationUUID + "/workbench/prompts", http.StatusOK},
		} {
			response := app.platformRequestWithHeaders(t, http.MethodGet, tc.path, nil, cookies, map[string]string{"X-Workspace-ID": workspace.ID, "anthropic-beta": "files-api-2025-04-14"})
			response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("%s: %d，期望 %d", tc.path, response.StatusCode, tc.status)
			}
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
	t.Run("计费提权与恢复幂等且无需继承记录", func(t *testing.T) {
		assertEffectiveWorkspaceRole(t, resolver, refs.OrganizationUUID, billingID, workspace.ID, "workspace_billing")

		adminCookies := workspaceUserCookies(t, app, refs.OrganizationUUID, principal.UserExternalID)
		billingCookies := workspaceUserCookies(t, app, refs.OrganizationUUID, billingID)
		for _, role := range []string{"workspace_admin", "workspace_admin", "workspace_billing", "workspace_billing"} {
			response := app.platformRequest(t, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID+"/members/"+billingID,
				strings.NewReader(`{"workspace_role":"`+role+`"}`), adminCookies)
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("角色更新: %d", response.StatusCode)
			}
			assertEffectiveWorkspaceRole(t, resolver, refs.OrganizationUUID, billingID, workspace.ID, role)
			response = app.platformRequestWithHeaders(t, http.MethodGet, "/v1/files?beta=true", nil, billingCookies,
				map[string]string{"X-Workspace-ID": workspace.ID, "anthropic-beta": "files-api-2025-04-14"})
			response.Body.Close()
			expected := http.StatusForbidden
			if role == "workspace_admin" {
				expected = http.StatusOK
			}
			if response.StatusCode != expected {
				t.Fatalf("资源请求: %d，期望 %d", response.StatusCode, expected)
			}
		}
		if _, err := app.db.GetAdminWorkspaceMember(ctx, refs.OrganizationUUID, workspace.ID, billingID); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("恢复继承后仍有显式成员: %v", err)
		}
	})
	t.Run("无显式授权的计费用户组织降级后失去普通空间", func(t *testing.T) {
		id := seedAdminUser(t, app.pool, "billing-downgrade-"+uniqueAdminSuffix()+"@example.local", "billing")
		assertEffectiveWorkspaceRole(t, resolver, refs.OrganizationUUID, id, workspace.ID, "workspace_billing")
		if _, err := app.db.UpdateAdminUserRole(ctx, refs.OrganizationUUID, id, "user"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := resolver.Resolve(ctx, refs.OrganizationUUID, id, workspace.ID); !errors.Is(err, workspaceaccess.ErrDenied) {
			t.Fatalf("降级未失去继承: %v", err)
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
}
