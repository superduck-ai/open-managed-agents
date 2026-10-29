package platformapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

type ObservabilityProvider interface {
	List(context.Context, observability.Scope, observability.Window, observability.ListQuery) (observability.List, error)
	Dashboard(context.Context, observability.Scope, observability.Window, observability.DashboardRequest) (observability.Dashboard, error)
	Page(context.Context, observability.Scope, observability.Window, observability.Target, int64) (observability.Page, error)
}

func RegisterOrganizationObservabilityRoutes(r chi.Router, provider ObservabilityProvider, logger *slog.Logger) {
	adapter := httpapi.NewErrorAdapter(logger)
	r.Get("/observability/dashboards/{tab}", adapter.Wrap(observabilityDashboardEndpoint(provider)))
	r.Get("/observability/trajectories", adapter.Wrap(trajectoryListEndpoint(provider)))
	r.Get("/observability/trajectories/{session_id}", adapter.Wrap(trajectoryDetailEndpoint(provider)))
}

func trajectoryListEndpoint(provider ObservabilityProvider) httpapi.Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		scope, window, err := trajectoryRequest(w, r)
		if err != nil || scope == nil {
			return err
		}
		query := r.URL.Query()
		offset, err := observability.ParseOffset(strings.TrimSpace(query.Get("offset")))
		if err != nil {
			return err
		}
		result, err := provider.List(r.Context(), *scope, window, observability.ListQuery{Offset: offset, Sort: strings.TrimSpace(query.Get("sort"))})
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
		return nil
	}
}

func observabilityDashboardEndpoint(provider ObservabilityProvider) httpapi.Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		scope, window, err := trajectoryRequest(w, r)
		if err != nil || scope == nil {
			return err
		}
		query := r.URL.Query()
		scope.AgentVersions, err = observability.ParseAgentVersions(query["agent_version"])
		if err != nil {
			return err
		}
		result, err := provider.Dashboard(r.Context(), *scope, window, observability.DashboardRequest{
			Tab:   chi.URLParam(r, "tab"),
			Model: strings.TrimSpace(query.Get("model")),
			Tool:  strings.TrimSpace(query.Get("tool")),
		})
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
		return nil
	}
}

func trajectoryDetailEndpoint(provider ObservabilityProvider) httpapi.Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		scope, window, err := trajectoryRequest(w, r)
		if err != nil || scope == nil {
			return err
		}
		query := r.URL.Query()
		after, err := observability.ParseCursor(strings.TrimSpace(query.Get("after_seq")))
		if err != nil {
			return err
		}
		target := observability.Target{
			SessionID: chi.URLParam(r, "session_id"),
			ThreadID:  strings.TrimSpace(query.Get("thread_id")),
			Segment:   strings.TrimSpace(query.Get("segment")),
		}
		scope.AgentID = ""
		result, err := provider.Page(r.Context(), *scope, window, target, after)
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
		return nil
	}
}

func trajectoryRequest(w http.ResponseWriter, r *http.Request) (*observability.Scope, observability.Window, error) {
	orgUUID, visible := visibleOrgUUID(w, r)
	if !visible {
		return nil, observability.Window{}, nil
	}
	scope, err := trajectoryScope(r, orgUUID)
	if err != nil {
		return nil, observability.Window{}, err
	}
	query := r.URL.Query()
	window, err := observability.ParseWindow(query.Get("start_time"), query.Get("end_time"))
	return &scope, window, err
}

func trajectoryScope(r *http.Request, orgUUID string) (observability.Scope, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.WorkspaceUUID == "" {
		return observability.Scope{}, trajectoryWorkspaceRequired()
	}
	query := r.URL.Query()
	return observability.Scope{OrganizationUUID: orgUUID, WorkspaceUUID: principal.WorkspaceUUID, AgentID: strings.TrimSpace(query.Get("agent_id")), SessionID: strings.TrimSpace(query.Get("session_id"))}, nil
}
