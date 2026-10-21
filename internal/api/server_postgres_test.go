//go:build integration

package api

import (
	"encoding/json"
	"errors"

	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestConsoleRoutesRequireCSRF(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := database.Seed(t.Context(), cfg.Bootstrap); err != nil {
		t.Fatal(err)
	}
	userID, organizationUUID, err := database.FindBootstrapUserContext(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := database.ResolvePlatformSessionIdentity(t.Context(), platformsession.CreateInput{
		SessionKey: "mcp-csrf-session", UserUUID: userID, OrgUUID: organizationUUID,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := platformsession.NewMemoryStore()
	if err := store.Save(t.Context(), "mcp-csrf-session", session); err != nil {
		t.Fatal(err)
	}
	credentials := newFilestoreAuthCredentials(t)
	server := NewServer(ServerDeps{
		Config: cfg, DB: database, PlatformStore: store,
		CodeSessionCredentials: credentials.ingress, FilestoreCredentials: credentials.filestore,
	})
	path := "/api/console/organizations/" + organizationUUID + "/workspaces/default/mcp_servers"
	name := "csrf-" + uuid.NewV4().String()
	body := `{"name":"` + name + `","url":"https://` + name + `.example.com/mcp"}`
	request := func(method, requestPath, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, requestPath, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "sessionKey", Value: "mcp-csrf-session"})
		r.Header.Set("Content-Type", "text/plain")
		r.Header.Set("Origin", "https://untrusted.example.com")
		r.Header.Set("X-CSRF-Token", token)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, r)
		return response
	}
	for _, route := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, path},
		{http.MethodPost, path + "/mcp_missing"},
		{http.MethodDelete, path + "/mcp_missing"},
		{http.MethodPost, "/api/console/organizations/" + organizationUUID + "/members/user_missing"},
		{http.MethodDelete, "/api/console/organizations/" + organizationUUID + "/members/user_missing"},
		{http.MethodPost, "/api/console/organizations/" + organizationUUID + "/workspaces/default/members"},
		{http.MethodPost, "/api/console/organizations/" + organizationUUID + "/workspaces/default/members/user_missing"},
		{http.MethodDelete, "/api/console/organizations/" + organizationUUID + "/workspaces/default/members/user_missing"},
		{http.MethodPost, "/api/console/organizations/" + organizationUUID + "/workspaces"},
		{http.MethodPatch, "/api/console/organizations/" + organizationUUID + "/workspaces/default"},
		{http.MethodPost, "/api/console/organizations/" + organizationUUID + "/workspaces/default/archive"},
	} {
		for _, token := range []string{"", "invalid", auth.PlatformCSRFToken("another-session")} {
			t.Run(route.method+route.path+"/"+token, func(t *testing.T) {
				response := request(route.method, route.path, token)
				if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "Invalid CSRF token") {
					t.Fatalf("response = %d %s, want CSRF rejection", response.Code, response.Body.String())
				}
			})
		}
	}
	servers, _, err := database.ListWorkspaceMCPServersPage(t.Context(), db.ListWorkspaceMCPServersPageParams{
		WorkspaceUUID: session.WorkspaceUUID, Search: name,
	})
	if err != nil || len(servers) != 0 {
		t.Fatalf("rejected requests created servers: %v, error: %v", servers, err)
	}
	token := auth.PlatformCSRFToken("mcp-csrf-session")
	created := request(http.MethodPost, path, token)
	if created.Code != http.StatusOK {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.ID == "" {
		t.Fatalf("create response = %s, error: %v", created.Body.String(), err)
	}
	path += "/" + result.ID
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		requestToken := token
		if method == http.MethodGet {
			requestToken = ""
		}
		response := request(method, path, requestToken)
		if response.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", method, response.Code, response.Body.String())
		}
	}
	if _, err := database.GetWorkspaceMCPServer(t.Context(), session.WorkspaceUUID, result.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleted server lookup error = %v, want not found", err)
	}
}
