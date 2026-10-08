//go:build e2e

package tests

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTunnelConsoleWorkspaceAuthorization(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = managedTunnelDatabase(t, cfg.Database.URL)
	app := newTestAppWithStore(t, &cfg, newFakeStore("review-scope"))
	t.Cleanup(app.close)
	cookies := app.platformLoginCookies(t, "review-scope@example.com")
	org := responseCookie(cookies, "lastActiveOrg").Value
	target := createPlatformWorkspace(t, app, cookies, org, "Other workspace")
	session, err := app.sessions.Get(t.Context(), responseCookie(cookies, "sessionKey").Value)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/console/organizations/" + org + "/workspaces/" + target + "/mcp_tunnels/"
	created := app.platformRequestWithHeaders(t, "POST", path, strings.NewReader(`{"display_name":"scope proof"}`), cookies, nil)
	if created.StatusCode != http.StatusOK {
		t.Fatalf("create status %d", created.StatusCode)
	}
	var tunnel struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(created.Body).Decode(&tunnel)
	created.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.db.UpdateAdminUserRole(t.Context(), org, session.UserExternalID, "user"); err != nil {
		t.Fatal(err)
	}

	for _, headers := range []map[string]string{nil, {"X-Workspace-ID": "default"}, {"X-Workspace-ID": target}} {
		for _, endpoint := range []struct{ method, suffix string }{{"GET", ""}, {"GET", tunnel.ID}, {"POST", ""}, {"POST", tunnel.ID + "/reveal_token"}, {"POST", tunnel.ID + "/rotate_token"}, {"POST", tunnel.ID + "/archive"}, {"POST", tunnel.ID + "/probe"}} {
			response := app.platformRequestWithHeaders(t, endpoint.method, path+endpoint.suffix, strings.NewReader(`{}`), cookies, headers)
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("%s %s with %v returned %d, want 403", endpoint.method, endpoint.suffix, headers, response.StatusCode)
			}
		}
	}
	workspace, err := app.db.GetAdminWorkspace(t.Context(), org, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.db.CreateAdminWorkspaceMember(t.Context(), db.AdminWorkspaceMember{ExternalID: "wmem_tunnel_review", OrganizationUUID: org, WorkspaceUUID: workspace.UUID, WorkspaceExternalID: workspace.ExternalID, UserUUID: session.UserUUID, UserExternalID: session.UserExternalID, WorkspaceRole: "workspace_developer", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	response := app.platformRequestWithHeaders(t, "GET", path, nil, cookies, nil)
	body := readAll(t, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), tunnel.ID) {
		t.Fatalf("member list returned %d", response.StatusCode)
	}
	response = app.platformRequestWithHeaders(t, "GET", "/api/console/organizations/"+org+"/workspaces/default/mcp_tunnels/", nil, cookies, map[string]string{"X-Workspace-ID": target})
	body = readAll(t, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || strings.Contains(string(body), tunnel.ID) {
		t.Fatal("header overrode URL default workspace")
	}

}
