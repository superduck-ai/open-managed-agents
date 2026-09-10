package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

// 组织级回退读取已删除：任何工作区身份都只能读取当前 X-Workspace-ID 空间的 Memory 数据。
func TestMemoryReadsRejectOtherWorkspace(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("memory-workspace-isolation"))
	defer app.close()
	refs := getAdminDefaultIDs(t, app.pool)
	store := createMemoryStore(t, app, "隔离验收")
	memory := createMemory(t, app, store.ID, "/check.md", "只属于当前工作区")
	workspace := createAdminWorkspace(t, app, "记忆隔离-"+uniqueAdminSuffix(), nil, nil)
	userID := seedAdminUser(t, app.pool, "memory-"+uniqueAdminSuffix()+"@example.local", "admin")
	cookies := workspaceSessionCookies(t, app, refs.OrganizationUUID, userID)
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

func workspaceSessionCookies(t *testing.T, app *testApp, orgUUID, userID string) []*http.Cookie {
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
