package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestNewServerUsesDefaultLoggerForHTTPAccess(t *testing.T) {
	var logs bytes.Buffer
	previousDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousDefault)
	})

	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create code session credentials: %v", err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create filestore credentials: %v", err)
	}
	server := NewServer(ServerDeps{
		CodeSessionCredentials: credentials,
		FilestoreCredentials:   filestoreCredentials,
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	entries := parseSlogJSONLines(t, logs.String())
	if len(entries) != 2 {
		t.Fatalf("access log entries = %d, want 2: %s", len(entries), logs.String())
	}
	if entries[0]["component"] != "http" || entries[0]["msg"] != "http request" {
		t.Fatalf("unexpected request access log: %#v", entries[0])
	}
	if entries[1]["component"] != "http" || entries[1]["msg"] != "http response" {
		t.Fatalf("unexpected response access log: %#v", entries[1])
	}
}

func TestPlatformCSRFMiddlewareProtectsUnsafeRequests(t *testing.T) {
	t.Parallel()
	called := false
	handler := platformCSRFMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	missing := httptest.NewRequest(http.MethodPost, "/api/console/organizations/org", nil)
	missing.AddCookie(&http.Cookie{Name: "sessionKey", Value: "session-secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("missing CSRF response = %d, called = %v", response.Code, called)
	}

	valid := httptest.NewRequest(http.MethodPost, "/api/console/organizations/org", nil)
	valid.AddCookie(&http.Cookie{Name: "sessionKey", Value: "session-secret"})
	valid.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken("session-secret"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, valid)
	if response.Code != http.StatusOK || !called {
		t.Fatalf("valid CSRF response = %d, called = %v", response.Code, called)
	}
}

func TestReadinessReportsMissingDependencies(t *testing.T) {
	t.Parallel()
	server := &Server{}
	response := httptest.NewRecorder()
	server.handleReadiness(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if body := response.Body.String(); !bytes.Contains([]byte(body), []byte(`"database":"unavailable"`)) || !bytes.Contains([]byte(body), []byte(`"tunnel_nats":"unavailable"`)) || !bytes.Contains([]byte(body), []byte(`"tunnel_redis":"unavailable"`)) {
		t.Fatalf("readiness body = %s", body)
	}
}

func TestMCPServerRoutesRequireCSRF(t *testing.T) {
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
	if err := database.Seed(t.Context(), nil); err != nil {
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
