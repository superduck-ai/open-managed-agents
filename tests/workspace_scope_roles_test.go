package tests

import (
	"net/http"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// 既有计费账号也必须经过共用的组织、资源归属、归档与撤权检查。
func TestWorkspaceScopeChecksAllRoles(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("workspace-scope-roles"))
	defer app.close()
	ctx := t.Context()
	refs := getAdminDefaultIDs(t, app.pool)
	suffix := uniqueAdminSuffix()
	_, foreignWorkspace := seedWorkspaceKey(t, app.pool, "外部组织-"+suffix, "wrkspc_foreign_"+suffix, "key_foreign_"+suffix, "sk-foreign-"+suffix)
	store := createMemoryStore(t, app, "默认空间资源")
	for _, role := range []string{"developer", "billing"} {
		t.Run(role, func(t *testing.T) {
			userID := seedAdminUser(t, app.pool, role+suffix+"@example.local", role)
			user, err := app.db.GetAdminUser(ctx, refs.OrganizationUUID, userID)
			if err != nil {
				t.Fatal(err)
			}
			created := createAdminWorkspace(t, app, role+suffix, nil, nil)
			workspace, err := app.db.GetAdminWorkspace(ctx, refs.OrganizationUUID, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.db.CreateAdminWorkspaceMember(ctx, db.AdminWorkspaceMember{
				ExternalID: "wmem_" + role + suffix, OrganizationUUID: refs.OrganizationUUID, WorkspaceUUID: workspace.UUID,
				WorkspaceExternalID: workspace.ExternalID, UserUUID: user.UUID, UserExternalID: userID,
				WorkspaceRole: "workspace_admin", CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, userID)
			request := func(path, workspaceID string, status int) {
				t.Helper()
				response := app.platformRequestWithHeaders(t, http.MethodGet, path, nil, cookies, map[string]string{
					"X-Workspace-ID": workspaceID, "anthropic-beta": "managed-agents-2026-04-01,files-api-2025-04-14",
				})
				defer response.Body.Close()
				if response.StatusCode != status {
					t.Fatalf("%s %s: %d，期望 %d", path, workspaceID, response.StatusCode, status)
				}
			}
			request("/v1/files?beta=true", foreignWorkspace, http.StatusForbidden)
			request("/v1/organizations/users", workspace.ExternalID, http.StatusForbidden)
			request("/v1/memory_stores/"+store.ID+"?beta=true", workspace.ExternalID, http.StatusNotFound)
			request("/v1/files?beta=true", workspace.ExternalID, http.StatusOK)
			if _, err := app.db.ArchiveAdminWorkspace(ctx, refs.OrganizationUUID, workspace.ExternalID); err != nil {
				t.Fatal(err)
			}
			request("/v1/files?beta=true", workspace.ExternalID, http.StatusForbidden)
			if _, err := app.db.DeleteAdminUser(ctx, refs.OrganizationUUID, userID); err != nil {
				t.Fatal(err)
			}
			request("/v1/files?beta=true", "default", http.StatusForbidden)
		})
	}
}
