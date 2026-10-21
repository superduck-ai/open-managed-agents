package platformapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func RegisterOrganizationRuntimeCostRoutes(r chi.Router, database *db.DB) {
	r.Get("/runtime_cost", handleRuntimeCost(database))
}

func handleRuntimeCost(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgUUID, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		if r.URL.Query().Get("workspace_id") == "" {
			principal, allowed := auth.PrincipalFromContext(r.Context())
			if !allowed || !principal.WorkspaceAccess.ManageOrganization() {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "organization administrator required"})
				return
			}
		}
		query, err := runtimeCostQuery(r, orgUUID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid runtime cost query"})
			return
		}
		query.USDPerHour = database.RuntimeUSDPerHour()
		if query.USDPerHour == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"configured":       false,
				"currency":         "USD",
				"total":            "0.00",
				"daily":            []any{},
				"groups":           []any{},
				"models":           []string{},
				"service_accounts": []any{},
			})
			return
		}
		result, err := database.RuntimeCost(r.Context(), query)
		if errors.Is(err, db.ErrInvalidState) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid runtime cost query"})
			return
		}
		if err != nil {
			internalError(w, "runtime cost is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, runtimeCostResponse(result))
	}
}

func runtimeCostQuery(r *http.Request, organizationUUID string) (db.RuntimeCostQuery, error) {
	values := r.URL.Query()
	starting, err := time.Parse("2006-01-02", values.Get("starting_on"))
	if err != nil {
		return db.RuntimeCostQuery{}, err
	}
	ending, err := time.Parse("2006-01-02", values.Get("ending_before"))
	if err != nil {
		return db.RuntimeCostQuery{}, err
	}
	return db.RuntimeCostQuery{
		OrganizationUUID:    organizationUUID,
		WorkspaceExternalID: values.Get("workspace_id"),
		Model:               values.Get("model"),
		ServiceAccountID:    values.Get("service_account_id"),
		GroupBy:             values.Get("group_by"),
		StartingAt:          starting.UTC(),
		EndingBefore:        ending.UTC(),
	}, nil
}

func runtimeCostResponse(result db.RuntimeCost) map[string]any {
	daily := make([]map[string]any, 0, len(result.Daily))
	for _, day := range result.Daily {
		daily = append(daily, map[string]any{
			"date":   day.Date,
			"amount": day.Amount,
			"groups": runtimeCostGroupResponse(day.Groups),
		})
	}
	return map[string]any{
		"configured":       true,
		"currency":         result.Currency,
		"total":            result.Total,
		"daily":            daily,
		"groups":           runtimeCostGroupResponse(result.Groups),
		"models":           result.Models,
		"service_accounts": result.ServiceAccounts,
	}
}

func runtimeCostGroupResponse(groups []db.RuntimeCostGroup) []map[string]any {
	response := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		response = append(response, map[string]any{"id": group.ID, "label": group.Label, "amount": group.Amount})
	}
	return response
}
