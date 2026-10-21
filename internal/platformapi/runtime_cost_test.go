package platformapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestRuntimeCostWithoutWorkspaceRequiresOrganizationAdmin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/runtime_cost?starting_on=2026-10-01&ending_before=2026-10-02", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", "org")
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, route)
	ctx = auth.WithPrincipal(ctx, auth.Principal{
		OrganizationUUID: "org",
		WorkspaceAccess:  auth.WorkspaceAccess{OrganizationRole: "user"},
	})
	recorder := httptest.NewRecorder()

	handleRuntimeCost(nil).ServeHTTP(recorder, request.WithContext(ctx))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
