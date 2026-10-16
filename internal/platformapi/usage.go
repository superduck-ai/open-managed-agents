package platformapi

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/analytics"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func RegisterOrganizationUsageRoutes(r chi.Router, database *db.DB) {
	handler := usageHandler{database: database}
	r.Get("/usage_activities", handler.report)
	r.Get("/usage_cost", handler.report)
	r.Get("/workspaces/{workspaceId}/usage_cost", handler.report)
}

type usageHandler struct{ database *db.DB }

func (h usageHandler) report(w http.ResponseWriter, r *http.Request) {
	org, ok := visibleOrgUUID(w, r)
	if !ok {
		return
	}
	query, err := analytics.ParseQuery(r.URL.Query(), time.Now().UTC())
	if err != nil {
		writeUsageQueryError(w, err)
		return
	}
	pathWorkspace := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if pathWorkspace != "" {
		if query.Workspace != "" && query.Workspace != pathWorkspace {
			writeUsageScopeConflict(w)
			return
		}
		query.Workspace = pathWorkspace
	}
	workspaces, err := h.database.ListConsoleWorkspaces(r.Context(), org, true)
	if err != nil {
		internalError(w, "failed to load analytics workspaces")
		return
	}
	workspaces, err = accessibleConsoleWorkspaces(r, h.database, workspaces)
	if err != nil {
		workspaceAccessDenied(w)
		return
	}
	query.Data.OrganizationUUID = org
	for _, workspace := range workspaces {
		if query.Workspace == "" || query.Workspace == workspace.UUID || query.Workspace == workspace.ExternalID || (query.Workspace == "default" && workspace.IsDefault) {
			query.Data.WorkspaceUUIDs = append(query.Data.WorkspaceUUIDs, workspace.UUID)
		}
	}
	if query.Workspace != "" && len(query.Data.WorkspaceUUIDs) == 0 {
		workspaceAccessDenied(w)
		return
	}
	rows, err := h.database.AggregateUsage(r.Context(), query.Data)
	if err != nil {
		internalError(w, "failed to query analytics usage")
		return
	}
	options, err := h.options(r, query.Data, workspaces)
	if err != nil {
		internalError(w, "failed to query analytics options")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		analytics.Report
		Options usageOptions `json:"options"`
	}{Report: analytics.BuildReport(rows, query.Data), Options: options})
}

type usageOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type usageOptions struct {
	Models     []string      `json:"models"`
	APIKeys    []string      `json:"api_keys"`
	Workspaces []usageOption `json:"workspaces"`
	FirstSeen  *time.Time    `json:"first_seen"`
	Sources    []string      `json:"sources"`
}

func (h usageHandler) options(r *http.Request, query db.UsageQuery, workspaces []ConsoleWorkspace) (usageOptions, error) {
	options := usageOptions{Models: []string{}, APIKeys: []string{}, Workspaces: []usageOption{}, Sources: []string{"managed_agents", "api", "workbench"}}
	for _, workspace := range workspaces {
		options.Workspaces = append(options.Workspaces, usageOption{Value: workspace.ExternalID, Label: workspace.Name})
	}
	dimensions, err := h.database.UsageDimensions(r.Context(), query)
	if err != nil {
		return options, err
	}
	for _, dimension := range dimensions {
		if dimension.Model != "" {
			options.Models = append(options.Models, dimension.Model)
		}
		if dimension.APIKeyUUID != "" {
			options.APIKeys = append(options.APIKeys, dimension.APIKeyUUID)
		}
		if options.FirstSeen == nil || dimension.FirstSeen.Before(*options.FirstSeen) {
			options.FirstSeen = new(dimension.FirstSeen)
		}
	}
	for _, workspace := range query.WorkspaceUUIDs {
		providers, err := h.database.ListLLMProviders(r.Context(), query.OrganizationUUID, workspace)
		if err != nil {
			return options, err
		}
		for _, provider := range providers {
			options.Models = append(options.Models, provider.ModelIDs...)
		}
	}
	slices.Sort(options.Models)
	options.Models = slices.Compact(options.Models)
	slices.Sort(options.APIKeys)
	options.APIKeys = slices.Compact(options.APIKeys)
	return options, nil
}
