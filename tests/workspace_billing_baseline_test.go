package tests

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

// 对照 #346 的共同基线：普通空间需显式成员，更新计费角色写入记录，不恢复继承。
func TestExistingBillingWorkspaceBehavior(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("billing-baseline"))
	defer app.close()
	ctx := t.Context()
	refs := getAdminDefaultIDs(t, app.pool)
	app.seedPlatformSession(t, "billing-baseline-admin")
	session, err := app.sessions.Get(ctx, "billing-baseline-admin")
	if err != nil {
		t.Fatal(err)
	}
	principal := session.Principal()
	userID := seedAdminUser(t, app.pool, "billing-baseline-"+uniqueAdminSuffix()+"@example.local", "billing")
	workspace := createAdminWorkspace(t, app, "计费基线-"+uniqueAdminSuffix(), nil, nil)
	cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, userID)
	resolver := workspaceaccess.New(app.db)
	if _, _, err := resolver.Resolve(ctx, refs.OrganizationUUID, userID, workspace.ID); !errors.Is(err, workspaceaccess.ErrDenied) {
		t.Fatalf("未加入空间: %v", err)
	}
	if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, userID, "workspace_admin", "update"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("无成员更新: %v", err)
	}
	if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, userID, "workspace_user", "create"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"workspace_user", "workspace_billing"} {
		if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, userID, role, "update"); err != nil {
			t.Fatal(err)
		}
		member, err := app.db.GetAdminWorkspaceMember(ctx, refs.OrganizationUUID, workspace.ID, userID)
		if err != nil || member.WorkspaceRole != role {
			t.Fatalf("显式记录: %+v %v", member, err)
		}
		for _, path := range []string{"/v1/files?beta=true", "/api/organizations/" + refs.OrganizationUUID + "/workbench/prompts"} {
			response := app.platformRequestWithHeaders(t, http.MethodGet, path, strings.NewReader(""), cookies, map[string]string{"X-Workspace-ID": workspace.ID, "anthropic-beta": "files-api-2025-04-14"})
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s: %d", path, response.StatusCode)
			}
		}
	}
	if _, err := workspaceaccess.ChangeMember(ctx, app.db, principal, workspace.ID, userID, "", "delete"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolver.Resolve(ctx, refs.OrganizationUUID, userID, workspace.ID); !errors.Is(err, workspaceaccess.ErrDenied) {
		t.Fatalf("撤权未生效: %v", err)
	}
}
