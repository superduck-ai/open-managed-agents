package mcpservers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

func TestCreateWorkspaceMCPServerNormalizesAndScopesConfiguration(t *testing.T) {
	store := &mcpServerStoreStub{}
	handler := NewHandler(store, nil)
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", handler.RegisterRoutes)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers",
		strings.NewReader(`{"name":" internal-docs ","url":" HTTPS://Example.COM.:443/mcp "}`),
	)
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
		OrganizationUUID:    "org_test",
		WorkspaceUUID:       "workspace-uuid",
		WorkspaceExternalID: "workspace_test",
		UserUUID:            "user-uuid",
	}))
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if store.created == nil {
		t.Fatal("create store was not called")
	}
	if store.created.OrganizationUUID != "org_test" || store.created.WorkspaceUUID != "workspace-uuid" {
		t.Fatalf("created scope = (%q, %q), want principal scope", store.created.OrganizationUUID, store.created.WorkspaceUUID)
	}
	if store.created.Name != "internal-docs" || store.created.EndpointURL != "https://example.com/mcp" {
		t.Fatalf("created config = (%q, %q), want normalized values", store.created.Name, store.created.EndpointURL)
	}
	if !strings.HasPrefix(store.created.ExternalID, "mcp_") {
		t.Fatalf("created external ID = %q, want mcp_ prefix", store.created.ExternalID)
	}
	if !strings.Contains(recorder.Body.String(), `"type":"mcp_server"`) ||
		!strings.Contains(recorder.Body.String(), `"url":"https://example.com/mcp"`) {
		t.Fatalf("response = %s, want MCP server response", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"status"`) || strings.Contains(recorder.Body.String(), `"archived_at"`) {
		t.Fatalf("response = %s, does not want archive lifecycle fields", recorder.Body.String())
	}
}

func TestWorkspaceMCPServerRoutesKeepTenantAndSnapshotBoundaries(t *testing.T) {
	server := testMCPServer("mcpsrv_active", "docs", "https://docs.example.com/mcp")
	store := &mcpServerStoreStub{servers: []db.WorkspaceMCPServer{server}}
	handler := NewHandler(store, nil)
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", handler.RegisterRoutes)

	t.Run("list supports search", func(t *testing.T) {
		response := serveMCPServerRequest(router, http.MethodGet, "/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers?search=doc", "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"mcpsrv_active"`) {
			t.Fatalf("list response = %d %s", response.Code, response.Body.String())
		}
		if store.lastList.WorkspaceUUID != "workspace-uuid" || store.lastList.Search != "doc" {
			t.Fatalf("list params = %#v", store.lastList)
		}
	})

	t.Run("retrieve cannot cross workspace path", func(t *testing.T) {
		response := serveMCPServerRequest(router, http.MethodGet, "/api/console/organizations/org_test/workspaces/other/mcp_servers/mcpsrv_active", "")
		if response.Code != http.StatusNotFound || store.getCalls != 0 {
			t.Fatalf("cross-workspace response = %d calls=%d body=%s", response.Code, store.getCalls, response.Body.String())
		}
	})

	t.Run("update copies normalized future configuration only", func(t *testing.T) {
		response := serveMCPServerRequest(router, http.MethodPost, "/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers/mcpsrv_active", `{"name":" docs-v2 ","url":"https://DOCS.example.com:443/v2"}`)
		if response.Code != http.StatusOK || store.updated.Name != "docs-v2" || store.updated.EndpointURL != "https://docs.example.com/v2" {
			t.Fatalf("update response = %d body=%s updated=%#v", response.Code, response.Body.String(), store.updated)
		}
	})

	t.Run("archive route is not exposed", func(t *testing.T) {
		archiveResponse := serveMCPServerRequest(router, http.MethodPost, "/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers/mcpsrv_active/archive", "")
		if archiveResponse.Code != http.StatusNotFound {
			t.Fatalf("archive response = %d body=%s", archiveResponse.Code, archiveResponse.Body.String())
		}
	})

	t.Run("delete removes only the reusable directory entry", func(t *testing.T) {
		deleteResponse := serveMCPServerRequest(router, http.MethodDelete, "/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers/mcpsrv_active", "")
		if deleteResponse.Code != http.StatusOK || store.deletedID != "mcpsrv_active" || !strings.Contains(deleteResponse.Body.String(), `"deleted":true`) {
			t.Fatalf("delete response = %d body=%s", deleteResponse.Code, deleteResponse.Body.String())
		}
	})
}

func TestCreateWorkspaceMCPServerMapsValidationAndDuplicateErrors(t *testing.T) {
	store := &mcpServerStoreStub{createErr: db.ErrDuplicate}
	handler := NewHandler(store, nil)
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", handler.RegisterRoutes)

	invalid := serveMCPServerRequest(router, http.MethodPost, "/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers", `{"name":"bad__name","url":"https://example.com/mcp"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400: %s", invalid.Code, invalid.Body.String())
	}
	if !strings.Contains(invalid.Body.String(), `"error":"invalid_request"`) ||
		!strings.Contains(invalid.Body.String(), `"message":"name must use letters`) ||
		strings.Contains(invalid.Body.String(), `"type":"error"`) {
		t.Fatalf("invalid Console error response = %s", invalid.Body.String())
	}
	duplicate := serveMCPServerRequest(router, http.MethodPost, "/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers", `{"name":"docs","url":"https://example.com/mcp"}`)
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "already exists") {
		t.Fatalf("duplicate response = %d %s", duplicate.Code, duplicate.Body.String())
	}
	if !strings.Contains(duplicate.Body.String(), `"error":"conflict"`) || strings.Contains(duplicate.Body.String(), `"type":"error"`) {
		t.Fatalf("duplicate Console error response = %s", duplicate.Body.String())
	}
}

func TestCreateWorkspaceMCPServerRejectsInvalidEndpointInputs(t *testing.T) {
	store := &mcpServerStoreStub{}
	handler := NewHandler(store, nil)
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", handler.RegisterRoutes)

	tests := []struct {
		name string
		url  string
	}{
		{name: "unsupported protocol", url: "ftp://example.com/mcp"},
		{name: "embedded credentials", url: "https://user:secret@example.com/mcp"},
		{name: "fragment", url: "https://example.com/mcp#tools"},
		{
			name: "trimmed input over 2048 bytes even when normalization shortens it",
			url:  "https://example.com:443/" + strings.Repeat("a", 2025),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveMCPServerRequest(
				router,
				http.MethodPost,
				"/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers",
				`{"name":"docs","url":"`+test.url+`"}`,
			)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
		})
	}
	if store.created != nil {
		t.Fatalf("create store received invalid input: %+v", *store.created)
	}
}

func TestWorkspaceMCPServerRejectsMalformedPageCursorBeforeStore(t *testing.T) {
	store := &mcpServerStoreStub{}
	handler := NewHandler(store, nil)
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", handler.RegisterRoutes)

	invalidCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"created_at":"2026-08-13T00:00:00Z","uuid":"not-a-uuid"}`))
	response := serveMCPServerRequest(
		router,
		http.MethodGet,
		"/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers?page="+invalidCursor,
		"",
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d, want 400: %s", response.Code, response.Body.String())
	}
	if store.listCalls != 0 {
		t.Fatalf("list store calls = %d, want 0", store.listCalls)
	}
}

func TestWorkspaceMCPServerCursorRoundTrips(t *testing.T) {
	server := testMCPServer("mcpsrv_test", "docs", "https://docs.example.com/mcp")
	server.UUID = "00000000-0000-4000-8000-000000000003"
	cursor, err := decodeCursor(encodeCursor(server))
	if err != nil {
		t.Fatalf("decode encoded cursor: %v", err)
	}
	if cursor.UUID != server.UUID || !cursor.CreatedAt.Equal(server.CreatedAt) {
		t.Fatalf("cursor = %#v, want UUID %q and created_at %s", cursor, server.UUID, server.CreatedAt)
	}
}

func TestWorkspaceMCPServerAcceptsRecoveredOrganizationAliasOnlyOnTrustedHost(t *testing.T) {
	store := &mcpServerStoreStub{servers: []db.WorkspaceMCPServer{testMCPServer("mcpsrv_test", "docs", "https://docs.example.com/mcp")}}
	handler := NewHandler(store, nil)
	router := chi.NewRouter()
	router.Route("/api/console/organizations/{orgUuid}", handler.RegisterRoutes)

	request := httptest.NewRequest(http.MethodGet, "https://platform.claude.com/api/console/organizations/official-org/workspaces/workspace_test/mcp_servers", nil)
	ctx := auth.WithPrincipal(request.Context(), auth.Principal{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", WorkspaceExternalID: "workspace_test",
	})
	request = request.WithContext(auth.WithPlatformMirrorOrganizationAlias(ctx, "official-org"))
	trusted := httptest.NewRecorder()
	router.ServeHTTP(trusted, request)
	if trusted.Code != http.StatusOK {
		t.Fatalf("trusted alias status = %d, want 200: %s", trusted.Code, trusted.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "https://attacker.example/api/console/organizations/official-org/workspaces/workspace_test/mcp_servers", nil)
	ctx = auth.WithPrincipal(request.Context(), auth.Principal{
		OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid", WorkspaceExternalID: "workspace_test",
	})
	request = request.WithContext(auth.WithPlatformMirrorOrganizationAlias(ctx, "official-org"))
	untrusted := httptest.NewRecorder()
	router.ServeHTTP(untrusted, request)
	if untrusted.Code != http.StatusNotFound {
		t.Fatalf("untrusted alias status = %d, want 404: %s", untrusted.Code, untrusted.Body.String())
	}
}

func TestWorkspaceMCPServerConsoleErrorLogsSafeStructuredInternalFailure(t *testing.T) {
	privateCause := "database password must stay private"
	requestBody := `{"token":"body-secret"}`
	var logOutput bytes.Buffer
	handler := NewHandler(&mcpServerStoreStub{}, slog.New(slog.NewJSONHandler(&logOutput, nil)))
	request := httptest.NewRequest(
		http.MethodPost,
		"https://oma.example/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers?token=query-secret",
		strings.NewReader(requestBody),
	)
	request = request.WithContext(httpapi.WithRequestID(request.Context(), "req_mcp_test"))
	recorder := httptest.NewRecorder()

	handler.wrap(func(http.ResponseWriter, *http.Request) error {
		return apperr.New(apperr.Internal, "Could not create MCP server", errors.New(privateCause))
	})(recorder, request)

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "Could not create MCP server") {
		t.Fatalf("internal response = %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privateCause) {
		t.Fatalf("response leaked private cause: %s", recorder.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(logOutput.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want 1: %s", len(lines), logOutput.String())
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("decode structured log: %v: %s", err, lines[0])
	}
	for key, want := range map[string]string{
		"level": "ERROR", "msg": "workspace MCP server request failed",
		"request_id": "req_mcp_test", "method": http.MethodPost,
		"path":  "/api/console/organizations/org_test/workspaces/workspace_test/mcp_servers",
		"error": privateCause,
	} {
		if got := record[key]; got != want {
			t.Fatalf("log %s = %v, want %q: %#v", key, got, want, record)
		}
	}
	for _, secret := range []string{"query-secret", "body-secret"} {
		if strings.Contains(logOutput.String(), secret) {
			t.Fatalf("structured log leaked %q: %s", secret, logOutput.String())
		}
	}
}

type mcpServerStoreStub struct {
	created   *db.WorkspaceMCPServer
	createErr error
	servers   []db.WorkspaceMCPServer
	lastList  db.ListWorkspaceMCPServersPageParams
	listCalls int
	getCalls  int
	updated   db.WorkspaceMCPServer
	deletedID string
}

func (s *mcpServerStoreStub) CreateWorkspaceMCPServer(_ context.Context, server db.WorkspaceMCPServer) (db.WorkspaceMCPServer, error) {
	if s.createErr != nil {
		return db.WorkspaceMCPServer{}, s.createErr
	}
	createdInput := server
	s.created = &createdInput
	server.UUID = "mcp-server-uuid"
	server.CreatedAt = time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	server.UpdatedAt = server.CreatedAt
	return server, nil
}

func (s *mcpServerStoreStub) ListWorkspaceMCPServersPage(_ context.Context, params db.ListWorkspaceMCPServersPageParams) ([]db.WorkspaceMCPServer, bool, error) {
	s.listCalls++
	s.lastList = params
	rows := make([]db.WorkspaceMCPServer, 0, len(s.servers))
	for _, server := range s.servers {
		if params.Search != "" && !strings.Contains(strings.ToLower(server.Name+" "+server.EndpointURL), strings.ToLower(params.Search)) {
			continue
		}
		rows = append(rows, server)
	}
	return rows, false, nil
}

func (s *mcpServerStoreStub) GetWorkspaceMCPServer(_ context.Context, workspaceUUID, externalID string) (db.WorkspaceMCPServer, error) {
	s.getCalls++
	for _, server := range s.servers {
		if server.WorkspaceUUID == workspaceUUID && server.ExternalID == externalID {
			return server, nil
		}
	}
	return db.WorkspaceMCPServer{}, db.ErrNotFound
}

func (s *mcpServerStoreStub) UpdateWorkspaceMCPServer(_ context.Context, workspaceUUID, externalID string, next db.WorkspaceMCPServer) (db.WorkspaceMCPServer, error) {
	if workspaceUUID != "workspace-uuid" || externalID == "" {
		return db.WorkspaceMCPServer{}, errors.New("unexpected update scope")
	}
	next.ExternalID = externalID
	next.UUID = "mcp-server-uuid"
	next.CreatedAt = time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	next.UpdatedAt = next.CreatedAt.Add(time.Hour)
	s.updated = next
	return next, nil
}

func (s *mcpServerStoreStub) DeleteWorkspaceMCPServer(_ context.Context, workspaceUUID, externalID string) error {
	if workspaceUUID != "workspace-uuid" {
		return db.ErrNotFound
	}
	s.deletedID = externalID
	return nil
}

func serveMCPServerRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{
		OrganizationUUID:    "org_test",
		WorkspaceUUID:       "workspace-uuid",
		WorkspaceExternalID: "workspace_test",
		UserUUID:            "user-uuid",
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func testMCPServer(id, name, endpointURL string) db.WorkspaceMCPServer {
	now := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	return db.WorkspaceMCPServer{
		UUID: "uuid-" + id, ExternalID: id, OrganizationUUID: "org_test", WorkspaceUUID: "workspace-uuid",
		Name: name, TransportType: "url", EndpointURL: endpointURL, CreatedAt: now, UpdatedAt: now,
	}
}
