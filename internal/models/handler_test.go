package models

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestModelResponsesContainOnlyConfiguredIDs(t *testing.T) {
	models := modelResponses(llmproviders.NewCatalog([]db.LLMProvider{{Slug: "test", Models: db.LLMModels{{ID: "kimi-k2.5", Name: "Kimi"}, {ID: "qwen3-coder-plus"}}}}))
	if len(models) != 2 {
		t.Fatalf("modelResponses() len = %d", len(models))
	}
	if models[0].Type != "model" || models[0].ID != "test/kimi-k2.5" ||
		models[0].DisplayName != "Kimi" || models[0].CreatedAt != unknownModelCreatedAt ||
		models[0].Capabilities != nil || models[0].MaxInputTokens != nil || models[0].MaxTokens != nil ||
		models[1].Type != "model" || models[1].ID != "test/qwen3-coder-plus" ||
		models[1].DisplayName != "test/qwen3-coder-plus" || models[1].CreatedAt != unknownModelCreatedAt {
		t.Fatalf("models = %#v", models)
	}
}

func TestModelUnavailableDistinguishesMissingProviderFromLoadFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantKind apperr.Kind
		wantMsg  string
	}{
		{
			name:     "missing provider",
			err:      llmproviders.ErrNotConfigured,
			wantKind: apperr.Unavailable,
			wantMsg:  "This workspace has no LLM provider configured",
		},
		{
			name:     "load failure",
			err:      errors.New("database unavailable"),
			wantKind: apperr.Internal,
			wantMsg:  "Workspace model configuration is unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var appErr *apperr.Error
			if !errors.As(modelUnavailable(test.err), &appErr) {
				t.Fatal("expected application error")
			}
			if appErr.Kind != test.wantKind || appErr.PublicMessage != test.wantMsg {
				t.Fatalf("error = %+v", appErr)
			}
		})
	}
}

func retrieveTestCatalog() llmproviders.Catalog {
	return llmproviders.NewCatalog([]db.LLMProvider{
		{Slug: "test", Models: db.LLMModels{{ID: "kimi-k2.5", Name: "Kimi"}, {ID: "shared"}}},
		{Slug: "other", Models: db.LLMModels{{ID: "shared"}}},
		{Slug: "router", Models: db.LLMModels{{ID: "moonshotai/kimi"}}},
	})
}

func serveModels(t *testing.T, loader catalogLoader, authenticated bool, path string) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if authenticated {
		request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{OrganizationUUID: "org-uuid"}))
	}
	recorder := httptest.NewRecorder()
	http.StripPrefix("/v1/models", newHandler(loader)).ServeHTTP(recorder, request)
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return recorder.Code, body
}

func TestRetrieveModelFailures(t *testing.T) {
	staticCatalog := func(context.Context, string) (llmproviders.Catalog, error) { return retrieveTestCatalog(), nil }
	tests := []struct {
		name          string
		loader        catalogLoader
		authenticated bool
		path          string
		wantStatus    int
		wantType      string
		wantMessage   string
	}{
		{
			name:        "missing api key",
			loader:      staticCatalog,
			path:        "/v1/models/test/kimi-k2.5",
			wantStatus:  http.StatusUnauthorized,
			wantType:    "authentication_error",
			wantMessage: "Missing API key",
		},
		{
			name: "workspace has no provider",
			loader: func(context.Context, string) (llmproviders.Catalog, error) {
				return llmproviders.Catalog{}, llmproviders.ErrNotConfigured
			},
			authenticated: true,
			path:          "/v1/models/test/kimi-k2.5",
			wantStatus:    http.StatusServiceUnavailable,
			wantType:      "api_error",
			wantMessage:   "This workspace has no LLM provider configured",
		},
		{
			name:          "unknown qualified model",
			loader:        staticCatalog,
			authenticated: true,
			path:          "/v1/models/test/missing",
			wantStatus:    http.StatusNotFound,
			wantType:      "not_found_error",
			wantMessage:   "model: test/missing",
		},
		{
			name:          "unknown provider slug",
			loader:        staticCatalog,
			authenticated: true,
			path:          "/v1/models/missing/kimi-k2.5",
			wantStatus:    http.StatusNotFound,
			wantType:      "not_found_error",
			wantMessage:   "model: missing/kimi-k2.5",
		},
		{
			name:          "escaped percent is decoded once",
			loader:        staticCatalog,
			authenticated: true,
			path:          "/v1/models/test%25zz",
			wantStatus:    http.StatusNotFound,
			wantType:      "not_found_error",
			wantMessage:   "model: test%zz",
		},
		{
			name:          "ambiguous unqualified model",
			loader:        staticCatalog,
			authenticated: true,
			path:          "/v1/models/shared",
			wantStatus:    http.StatusBadRequest,
			wantType:      "invalid_request_error",
			wantMessage:   "test/shared, other/shared",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, body := serveModels(t, test.loader, test.authenticated, test.path)
			errorBody, _ := body["error"].(map[string]any)
			message, _ := errorBody["message"].(string)
			if status != test.wantStatus || errorBody["type"] != test.wantType || !strings.Contains(message, test.wantMessage) {
				t.Fatalf("status = %d, body = %#v", status, body)
			}
		})
	}
}

func TestRetrieveModelReturnsQualifiedModel(t *testing.T) {
	staticCatalog := func(context.Context, string) (llmproviders.Catalog, error) { return retrieveTestCatalog(), nil }
	tests := []struct {
		name            string
		path            string
		wantID          string
		wantDisplayName string
	}{
		{name: "qualified id", path: "/v1/models/test/kimi-k2.5", wantID: "test/kimi-k2.5", wantDisplayName: "Kimi"},
		{name: "unqualified unique id", path: "/v1/models/kimi-k2.5", wantID: "test/kimi-k2.5", wantDisplayName: "Kimi"},
		{name: "percent encoded slash", path: "/v1/models/test%2Fkimi-k2.5", wantID: "test/kimi-k2.5", wantDisplayName: "Kimi"},
		{name: "qualified ambiguous name", path: "/v1/models/other/shared", wantID: "other/shared", wantDisplayName: "other/shared"},
		{name: "upstream id with slash", path: "/v1/models/router/moonshotai/kimi", wantID: "router/moonshotai/kimi", wantDisplayName: "router/moonshotai/kimi"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, body := serveModels(t, staticCatalog, true, test.path)
			if status != http.StatusOK || body["type"] != "model" || body["id"] != test.wantID ||
				body["display_name"] != test.wantDisplayName || body["created_at"] != unknownModelCreatedAt {
				t.Fatalf("status = %d, body = %#v", status, body)
			}
			for _, field := range []string{"capabilities", "max_input_tokens", "max_tokens"} {
				if value, exists := body[field]; !exists || value != nil {
					t.Fatalf("%s = %#v, want explicit null", field, value)
				}
			}
		})
	}
}

func TestListModelsRouteStillServesCollection(t *testing.T) {
	staticCatalog := func(context.Context, string) (llmproviders.Catalog, error) { return retrieveTestCatalog(), nil }
	status, body := serveModels(t, staticCatalog, true, "/v1/models")
	data, _ := body["data"].([]any)
	if status != http.StatusOK || len(data) != 4 || body["first_id"] != "test/kimi-k2.5" || body["last_id"] != "router/moonshotai/kimi" {
		t.Fatalf("status = %d, body = %#v", status, body)
	}
}
