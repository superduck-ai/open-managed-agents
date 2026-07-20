package platformapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type UsageActivitiesResponse struct {
	Usages      map[string]any `json:"usages"`
	Granularity string         `json:"granularity"`
}

type APIKeysUsageResponse struct {
	APIKeys    []any `json:"api_keys"`
	CutoffDays int   `json:"cutoff_days"`
}

const (
	defaultPlatformCreditBalance = 100
	defaultPlatformMonthlyLimit  = 50000
)

type adminRequestStore interface {
	ListAdminRequests(ctx context.Context, orgUUID string, requestType string, status string, limit int) ([]AdminRequest, error)
}

type platformOrganizationGetter interface {
	GetPlatformOrganization(ctx context.Context, orgUUID string) (*OrganizationRecord, error)
}

type platformOrganizationUpdater interface {
	UpdatePlatformOrganization(ctx context.Context, orgUUID string, patch OrganizationUpdatePatch) (*OrganizationRecord, error)
}

func RegisterPlatformAccountRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/api/bootstrap", handleBootstrap(store))
	r.Get("/api/bootstrap/{orgUuid}/app_start", handleBootstrap(store))
	r.Get("/api/banners", handleBanners)
}

func RegisterPlatformBillingRoutes(r chi.Router) {
	r.Get("/api/billing/stripe_region", handleStripeRegion)
}

func RegisterOrganizationOnboardingRoutes(r chi.Router) {
	r.Get("/console_onboarding/tasks", handleConstant(map[string]any{"tasks": []any{}, "panel_state": "open", "panel_updated_at": nil}))
	r.Get("/setup_required", handleConstant(map[string]any{"setup_required": false}))
}

func RegisterOrganizationExperienceRoutes(r chi.Router) {
	r.Get("/experiences/{experienceType}", handleOrganizationExperienceType)
}

func RegisterOrganizationRootRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/", handleGetPlatformOrganization(store))
	r.Put("/", handleUpdatePlatformOrganization(store))
}

func RegisterOrganizationBillingRoutes(r chi.Router) {
	r.Get("/current_spend", handleCurrentSpend)
	r.Get("/rate_limits", handleRateLimits)
	r.Get("/prepaid/credits", handleConstant(map[string]any{
		"amount": defaultPlatformCreditBalance, "currency": "usd", "auto_reload_settings": nil,
		"pending_invoice_amount_cents": nil, "last_paid_purchase_cents": nil,
	}))
	r.Get("/api_keys/usage", handleAPIKeysUsage)
	r.Get("/workspaces/{workspaceId}/api_keys/usage", handleAPIKeysUsage)
	r.Get("/usage_activities", handleUsageActivities)
	r.Get("/usage_cost", handleUsageCost)
	r.Get("/workspaces/{workspaceId}/usage_cost", handleUsageCost)
	r.Get("/cache_analytics", handlePromptCacheAnalytics)
}

func RegisterOrganizationAnalyticsRoutes(r chi.Router) {
	r.Get("/analytics/sessions/overview", handleSessionAnalyticsOverview)
	r.Get("/analytics/sessions/timeseries", handleSessionAnalyticsTimeseries)
}

func RegisterConsoleOrganizationWorkspaceRoutes(r chi.Router, store OrganizationStore, logger *slog.Logger) {
	r.Get("/workspaces", handleListConsoleWorkspaces(store, logger))
}

func RegisterConsoleOrganizationAdminRequestRoutes(r chi.Router, store OrganizationStore) {
	r.Get("/admin_requests/join_org", handleListAdminRequests(store, "join_org"))
}

func handleConstant(body any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	}
}

func handleCurrentSpend(w http.ResponseWriter, _ *http.Request) {
	resetsAt := nextMonthlyResetAt(time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]any{
		"amount":         0,
		"balance":        defaultPlatformCreditBalance,
		"credit_balance": defaultPlatformCreditBalance,
		"current_spend":  0,
		"limit":          defaultPlatformMonthlyLimit,
		"monthly_limit":  defaultPlatformMonthlyLimit,
		"spend":          0,
		"total":          0,
		"resets_at":      resetsAt,
	})
}

func handleSessionAnalyticsOverview(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions_count":            analyticsValue(0),
		"error_rate":                analyticsValue(0),
		"input_tokens":              analyticsMetricBucket(),
		"output_tokens":             analyticsMetricBucket(),
		"duration":                  analyticsMetricBucket(),
		"active_time":               analyticsMetricBucket(),
		"input_tokens_per_session":  analyticsMetricBucket(),
		"output_tokens_per_session": analyticsMetricBucket(),
		"turns_per_session":         analyticsMetricBucket(),
		"tool_call_counts":          map[string]any{},
		"stop_reason_counts":        map[string]any{},
		"data_as_of":                nil,
	})
}

func handleSessionAnalyticsTimeseries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data":        []any{},
		"data_points": []any{},
		"group_by":    strings.TrimSpace(r.URL.Query().Get("group_by")),
	})
}

func analyticsMetricBucket() map[string]any {
	return map[string]any{
		"total": analyticsValue(0),
		"p50":   analyticsValue(0),
		"p90":   analyticsValue(0),
		"p95":   analyticsValue(0),
	}
}

func analyticsValue(value int) map[string]any {
	return map[string]any{"value": value}
}

func nextMonthlyResetAt(now time.Time) string {
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

func usageGranularity(r *http.Request) string {
	granularity := strings.TrimSpace(r.URL.Query().Get("granularity"))
	if granularity == "" {
		granularity = "daily"
	}
	return granularity
}

func handleUsageActivities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, UsageActivitiesResponse{
		Usages:      map[string]any{},
		Granularity: usageGranularity(r),
	})
}

func handleUsageCost(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"costs":                map[string]any{},
		"web_search_costs":     map[string]any{},
		"code_execution_costs": map[string]any{},
		"session_usage_costs":  map[string]any{},
		"claude_code_savings":  map[string]any{},
	})
}

func handleAPIKeysUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := visibleOrgUUID(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, APIKeysUsageResponse{APIKeys: []any{}, CutoffDays: 30})
}

func handlePromptCacheAnalytics(w http.ResponseWriter, r *http.Request) {
	if !visibleOrgUUIDOrPlatformClaudeMirror(w, r) {
		return
	}
	emptyUsage := map[string]int{
		"cache_read":     0,
		"cache_write_5m": 0,
		"cache_write_1h": 0,
		"uncached":       0,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": map[string]any{
			"current":  emptyUsage,
			"previous": emptyUsage,
		},
		"sparkline":     []any{},
		"group_stats":   []any{},
		"series":        []any{},
		"display_names": map[string]any{},
	})
}

func handleStripeRegion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"stripe_region": "us"})
}

func handleBanners(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{})
}

func handleGetPlatformOrganization(store OrganizationStore) http.HandlerFunc {
	organizationStore, _ := store.(platformOrganizationGetter)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if organizationStore == nil {
			internalError(w, "failed to load organization")
			return
		}
		org, err := organizationStore.GetPlatformOrganization(r.Context(), orgUUID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				organizationNotFound(w)
				return
			}
			internalError(w, "failed to load organization")
			return
		}
		writeJSON(w, http.StatusOK, buildOrganization(*org))
	}
}

func handleUpdatePlatformOrganization(store OrganizationStore) http.HandlerFunc {
	organizationStore, _ := store.(platformOrganizationUpdater)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if organizationStore == nil {
			internalError(w, "failed to update organization")
			return
		}
		body, err := readJSONObject(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body must be an object"})
			return
		}
		patch, ok := normalizeOrganizationUpdatePatch(w, body)
		if !ok {
			return
		}
		org, err := organizationStore.UpdatePlatformOrganization(r.Context(), orgUUID, patch)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				organizationNotFound(w)
				return
			}
			internalError(w, "failed to update organization")
			return
		}
		writeJSON(w, http.StatusOK, buildOrganization(*org))
	}
}

func normalizeOrganizationUpdatePatch(w http.ResponseWriter, body map[string]any) (OrganizationUpdatePatch, bool) {
	patch := OrganizationUpdatePatch{}
	if value, ok := body["name"]; ok {
		name, ok := value.(string)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name must be a string"})
			return OrganizationUpdatePatch{}, false
		}
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name cannot be empty"})
			return OrganizationUpdatePatch{}, false
		}
		patch.Name = &trimmed
	}
	if value, ok := body["settings"]; ok {
		settings, ok := value.(map[string]any)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "settings must be an object"})
			return OrganizationUpdatePatch{}, false
		}
		patch.Settings = cloneAnyMap(settings)
	}
	if value, ok := body["default_workspace_settings"]; ok {
		defaultWorkspaceSettings, ok := normalizeDefaultWorkspaceSettings(w, value)
		if !ok {
			return OrganizationUpdatePatch{}, false
		}
		if patch.Settings == nil {
			patch.Settings = map[string]any{}
		}
		patch.Settings["default_workspace_settings"] = defaultWorkspaceSettings
	}
	return patch, true
}

func normalizeDefaultWorkspaceSettings(w http.ResponseWriter, value any) (map[string]any, bool) {
	body, ok := value.(map[string]any)
	if !ok || body == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "default_workspace_settings must be an object"})
		return nil, false
	}
	out := cloneAnyMap(body)
	if value, ok := out["enable_api_keys"]; ok {
		if _, ok := value.(bool); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "enable_api_keys must be a boolean"})
			return nil, false
		}
	}
	return out, true
}

func handleListAdminRequests(store OrganizationStore, requestType string) http.HandlerFunc {
	adminStore, _ := store.(adminRequestStore)
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if adminStore == nil {
			internalError(w, "failed to list admin requests")
			return
		}
		requests, err := adminStore.ListAdminRequests(r.Context(), orgUUID, requestType, "pending", 1000)
		if err != nil {
			internalError(w, "failed to list admin requests")
			return
		}
		out := make([]map[string]any, 0, len(requests))
		for _, request := range requests {
			out = append(out, formatAdminRequest(request))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func formatAdminRequest(request AdminRequest) map[string]any {
	details := any(nil)
	if request.Details != nil {
		details = request.Details
	}
	var requester any
	if request.RequesterUUID != nil {
		requester = map[string]any{
			"uuid":          *request.RequesterUUID,
			"full_name":     optionalStringValue(request.RequesterName),
			"email_address": optionalStringValue(request.RequesterEmail),
			"role":          optionalStringValue(request.RequesterRole),
			"seat_tier":     optionalStringValue(request.RequesterSeatTier),
		}
	}
	return map[string]any{
		"uuid":                request.UUID,
		"request_type":        request.RequestType,
		"status":              request.Status,
		"created_at":          isoTime(request.CreatedAt),
		"resolved_at":         optionalTimeString(request.ResolvedAt),
		"requester_uuid":      optionalStringValue(request.RequesterUUID),
		"requester_email":     optionalStringValue(request.RequesterEmail),
		"requester":           requester,
		"requested_seat_tier": optionalStringValue(request.RequestedSeatTier),
		"details":             details,
		"context":             detailField(request.Details, "context"),
		"daily_usage":         detailField(request.Details, "daily_usage"),
		"capped_day_count":    detailField(request.Details, "capped_day_count"),
		"period_total_days":   detailField(request.Details, "period_total_days"),
	}
}

func detailField(details map[string]any, key string) any {
	if details == nil {
		return nil
	}
	if value, ok := details[key]; ok {
		return value
	}
	return nil
}
