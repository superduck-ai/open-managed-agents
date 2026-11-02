package telemetrydestinations

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func serve(t *testing.T, handler http.Handler, method, path, body string, principal *auth.Principal, beta bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if beta {
		request.Header.Set("anthropic-beta", "other, "+BetaHeader)
	}
	if principal != nil {
		request = request.WithContext(auth.WithPrincipal(request.Context(), *principal))
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestHandlerRequiresBetaAndOrganizationRoles(t *testing.T) {
	handler := NewHandler(NewService(newMemoryStore(), &recordingSealer{}, &fixedTester{}, false), nil)
	admin := &auth.Principal{OrganizationUUID: "org", WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin"}}
	developer := &auth.Principal{OrganizationUUID: "org", WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "developer"}}
	apiKey := &auth.Principal{OrganizationUUID: "org"}
	body := `{"endpoint_url":"https://collector.example.com"}`
	for _, test := range []struct {
		name      string
		method    string
		path      string
		principal *auth.Principal
		beta      bool
		status    int
		message   string
	}{
		{"missing beta", http.MethodGet, "/", admin, false, http.StatusBadRequest, BetaHeader},
		{"missing principal", http.MethodGet, "/", nil, true, http.StatusNotFound, "organization not found"},
		{"API key without organization role", http.MethodGet, "/", apiKey, true, http.StatusNotFound, "organization not found"},
		{"developer cannot create", http.MethodPost, "/", developer, true, http.StatusForbidden, "administrator"},
		{"developer cannot test", http.MethodPost, "/tdst_x/test", developer, true, http.StatusForbidden, "administrator"},
		{"invalid limit", http.MethodGet, "/?limit=0", developer, true, http.StatusBadRequest, "limit"},
		{"unknown destination", http.MethodGet, "/tdst_missing", developer, true, http.StatusNotFound, "tdst_missing"},
		{"malformed body", http.MethodPost, "/", admin, true, http.StatusBadRequest, "JSON object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := body
			if test.name == "malformed body" {
				payload = "[]"
			}
			response := serve(t, handler, test.method, test.path, payload, test.principal, test.beta)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
		})
	}
	created := serve(t, handler, http.MethodPost, "/", body, admin, true)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), `"type":"telemetry_destination"`) {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	listed := serve(t, handler, http.MethodGet, "/?limit=5", "", developer, true)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"next_page":null`) || !strings.Contains(listed.Body.String(), "tdst_") {
		t.Fatalf("developer list = %d %s", listed.Code, listed.Body.String())
	}
}
