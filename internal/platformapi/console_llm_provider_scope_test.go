package platformapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestLLMProviderOrganizationScope(t *testing.T) {
	for _, tc := range []struct {
		name, method, principalOrg string
		want                       bool
		status                     int
	}{
		{"other organization", http.MethodPost, "other", false, http.StatusNotFound},
		{"member cannot delete", http.MethodDelete, "org", false, http.StatusForbidden},
		{"member can create", http.MethodPost, "org", true, http.StatusOK},
		{"member can update", http.MethodPut, "org", true, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			route := chi.NewRouteContext()
			route.URLParams.Add("orgUuid", "org")
			ctx := auth.WithPrincipal(req.Context(), auth.Principal{OrganizationUUID: tc.principalOrg})
			req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
			response := httptest.NewRecorder()
			_, _, ok := llmProviderScope(response, req, nil)
			if ok != tc.want || response.Code != tc.status {
				t.Fatalf("scope = %v, status = %d", ok, response.Code)
			}
		})
	}
}
