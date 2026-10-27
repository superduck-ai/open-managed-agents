package platformapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/observability"

	"github.com/go-chi/chi/v5"
)

type ObservabilityProvider interface {
	Dashboard(ctx context.Context) (observability.DashboardProjection, error)
	QueryPanel(ctx context.Context, scope observability.TenantScope, queryRef string, variables map[string]any) (observability.PanelResult, error)
	ListTraces(ctx context.Context, scope observability.TenantScope, variables map[string]any, offset int) (observability.TraceListResult, error)
	GetTrace(ctx context.Context, scope observability.TenantScope, traceID string, variables map[string]any) (observability.TraceDetailResult, error)
}

type panelQueryRequest struct {
	QueryRef  string         `json:"query_ref"`
	Variables map[string]any `json:"variables"`
}

const maxObservabilityPanelQueryBodyBytes = 64 << 10

func RegisterOrganizationObservabilityRoutes(r chi.Router, provider ObservabilityProvider, logger *slog.Logger) {
	adapter := httpapi.NewErrorAdapter(logger)
	r.Get("/observability/dashboard", adapter.Wrap(handleObservabilityDashboard(provider)))
	r.Post("/observability/panels/query", adapter.Wrap(handleObservabilityPanelQuery(provider)))
	r.Get("/observability/traces", adapter.Wrap(handleObservabilityTraceList(provider)))
	r.Get("/observability/traces/{traceId}", adapter.Wrap(handleObservabilityTraceDetail(provider)))
}

func handleObservabilityDashboard(provider ObservabilityProvider) httpapi.Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		if _, err := observabilityScope(r); err != nil {
			return err
		}
		dashboard, err := provider.Dashboard(r.Context())
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, dashboard)
		return nil
	}
}

func handleObservabilityPanelQuery(provider ObservabilityProvider) httpapi.Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		scope, err := observabilityScope(r)
		if err != nil {
			return err
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxObservabilityPanelQueryBodyBytes)
		req, err := readRequiredJSON[panelQueryRequest](r, true)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return observability.QueryRequestTooLarge()
			}
			return observability.InvalidRequestBody()
		}
		result, err := provider.QueryPanel(r.Context(), scope, req.QueryRef, req.Variables)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	}
}

func handleObservabilityTraceList(provider ObservabilityProvider) httpapi.Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		scope, err := observabilityScope(r)
		if err != nil {
			return err
		}
		offset, err := observability.ParseOffset(r.URL.Query().Get("offset"))
		if err != nil {
			return err
		}
		result, err := provider.ListTraces(r.Context(), scope, traceVariables(r), offset)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	}
}

func handleObservabilityTraceDetail(provider ObservabilityProvider) httpapi.Endpoint {
	return func(w http.ResponseWriter, r *http.Request) error {
		scope, err := observabilityScope(r)
		if err != nil {
			return err
		}
		traceID := strings.TrimSpace(chi.URLParam(r, "traceId"))
		result, err := provider.GetTrace(r.Context(), scope, traceID, traceDetailVariables(r))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	}
}

func observabilityScope(r *http.Request) (observability.TenantScope, error) {
	orgUUID, visible := resolvedVisibleOrgUUID(r)
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !visible || !ok {
		return observability.TenantScope{}, observability.OrganizationNotFound()
	}
	if strings.TrimSpace(principal.WorkspaceUUID) == "" {
		return observability.TenantScope{}, observability.WorkspaceRequired()
	}
	return observability.TenantScope{
		OrganizationUUID: orgUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
	}, nil
}

func traceVariables(r *http.Request) map[string]any {
	query := r.URL.Query()
	variables := map[string]any{}
	copyQueryVar(variables, query.Get("start_time"), "start_time")
	copyQueryVar(variables, query.Get("end_time"), "end_time")
	copyQueryVar(variables, query.Get("agent_id"), "agent_id")
	copyQueryVar(variables, query.Get("session_id"), "session_id")
	copyQueryVar(variables, query.Get("trace_id"), "trace_id")
	copyQueryVar(variables, query.Get("status"), "status")
	copyQueryIntList(variables, query["agent_version"], "agent_version")
	return variables
}

func traceDetailVariables(r *http.Request) map[string]any {
	query := r.URL.Query()
	variables := map[string]any{}
	copyQueryVar(variables, query.Get("start_time"), "start_time")
	copyQueryVar(variables, query.Get("end_time"), "end_time")
	copyQueryVar(variables, query.Get("agent_id"), "agent_id")
	copyQueryVar(variables, query.Get("session_id"), "session_id")
	copyQueryIntList(variables, query["agent_version"], "agent_version")
	return variables
}

func copyQueryVar(variables map[string]any, raw, name string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	variables[name] = raw
}

func copyQueryIntList(variables map[string]any, values []string, name string) {
	items := make([]any, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			items = append(items, part)
		}
	}
	if len(items) > 0 {
		variables[name] = items
	}
}
