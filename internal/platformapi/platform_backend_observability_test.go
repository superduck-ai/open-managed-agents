package platformapi

import (
	"context"
	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingTrajectoryProvider struct {
	scope     observability.Scope
	query     observability.ListQuery
	dashboard observability.DashboardRequest
	versions  []int
	target    observability.Target
	after     int64
}

func (p *recordingTrajectoryProvider) List(_ context.Context, scope observability.Scope, _ observability.Window, query observability.ListQuery) (observability.List, error) {
	p.scope = scope
	p.query = query
	return observability.List{Items: []observability.SessionTrajectory{}}, nil
}
func (p *recordingTrajectoryProvider) Dashboard(_ context.Context, scope observability.Scope, _ observability.Window, request observability.DashboardRequest) (observability.Dashboard, error) {
	p.scope = scope
	p.versions = scope.AgentVersions
	p.dashboard = request
	return observability.Dashboard{}, nil
}
func (p *recordingTrajectoryProvider) Page(_ context.Context, scope observability.Scope, _ observability.Window, target observability.Target, after int64) (observability.Page, error) {
	p.scope = scope
	p.target = target
	p.after = after
	return observability.Page{}, nil
}

func TestTrajectoryRoutes(t *testing.T) {
	provider := &recordingTrajectoryProvider{}
	router := chi.NewRouter()
	router.Route("/api/organizations/{orgUuid}", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{OrganizationUUID: "org_1", WorkspaceUUID: "ws_1"})))
			})
		})
		RegisterOrganizationObservabilityRoutes(r, provider, nil)
	})
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/trajectories/id?after_seq=oops", 400},
		{"/trajectories?offset=-1", 400},
		{"/trajectories?offset=100001", 400},
		{"/trajectories?offset=999999999999999999999", 400},
		{"/trajectories/id?after_seq=-2", 400},
		{"/trajectories?start_time=2020-01-01T00:00:00Z&end_time=2022-01-01T00:00:00Z", 400},
		{"/trajectories?start_time=invalid", 400},
		{"/traces", 404},
		{"/dashboard", 404},
		{"/overview", 404},
		{"/dashboards/models?start_time=invalid", 400},
		{"/dashboards/models?agent_id=agent_1&agent_version=0", 400},
		{"/dashboards/models?agent_id=agent_1&agent_version=2&agent_version=1&model=%20model-a%20&tool=Bash", 200},
		{"/trajectories?agent_id=agent_1&sort=%20errors", 200},
		{"/trajectories/sesn_1?after_seq=20&thread_id=%20sthr_1%20&segment=%20sthr_1&agent_id=agent_1", 200},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability"+test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s: status %d: %s", test.path, response.Code, response.Body.String())
		}
	}
	if provider.scope.OrganizationUUID != "org_1" || provider.scope.WorkspaceUUID != "ws_1" || provider.scope.AgentID != "" || provider.after != 20 {
		t.Fatalf("invalid scope or cursor: %+v", provider)
	}
	if provider.dashboard != (observability.DashboardRequest{Tab: "models", Model: "model-a", Tool: "Bash"}) || len(provider.versions) != 2 || provider.versions[0] != 1 || provider.query.Sort != "errors" {
		t.Fatalf("dashboard = %+v versions = %v, list query = %+v", provider.dashboard, provider.versions, provider.query)
	}
	if provider.target != (observability.Target{SessionID: "sesn_1", ThreadID: "sthr_1", Segment: "sthr_1"}) {
		t.Fatalf("invalid trajectory target: %+v", provider.target)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/organizations/org_1/observability/trajectories?agent_id=agent_1&session_id=%20sesn_1%20", nil))
	if response.Code != 200 || provider.scope.AgentID != "agent_1" || provider.scope.SessionID != "sesn_1" {
		t.Fatalf("list scope = %+v", provider.scope)
	}
}

func TestTrajectoryVisibilityAndWorkspaceErrors(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal *auth.Principal
		org       string
		status    int
		message   string
	}{
		{"invisible organization", &auth.Principal{OrganizationUUID: "other", WorkspaceUUID: "ws_1"}, "org_1", 404, "organization not found"},
		{"missing principal", nil, "org_1", 404, "organization not found"},
		{"missing workspace", &auth.Principal{OrganizationUUID: "org_1"}, "org_1", 400, "workspace is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, suffix := range []string{"/trajectories", "/trajectories/id", "/dashboards/overview"} {
				provider := &recordingTrajectoryProvider{}
				router := chi.NewRouter()
				router.Route("/api/organizations/{orgUuid}", func(r chi.Router) { RegisterOrganizationObservabilityRoutes(r, provider, nil) })
				request := httptest.NewRequest(http.MethodGet, "/api/organizations/"+test.org+"/observability"+suffix, nil)
				if test.principal != nil {
					request = request.WithContext(auth.WithPrincipal(request.Context(), *test.principal))
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) {
					t.Fatalf("%s: %d %s", suffix, response.Code, response.Body.String())
				}
				if provider.scope.OrganizationUUID != "" {
					t.Fatal("queried storage before authorization")
				}
			}
		})
	}
}
