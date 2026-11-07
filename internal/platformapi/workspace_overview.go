package platformapi

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type workspaceOverviewStore interface {
	consoleWorkspaceLister
	AggregateWorkspaceOverview(context.Context, db.WorkspaceOverviewQuery) ([]db.WorkspaceOverviewBucket, error)
	CountWorkspaceOverviewAgents(context.Context, string, string) (int64, error)
}

type overviewAgent struct {
	AgentID            string    `json:"agent_id"`
	Name               string    `json:"name"`
	SessionCount       int64     `json:"session_count"`
	FailedSessionCount int64     `json:"failed_session_count"`
	LastRunAt          time.Time `json:"last_run_at"`
}

type overviewDay struct {
	Date         string `json:"date"`
	AgentID      string `json:"agent_id"`
	SessionCount int64  `json:"session_count"`
}

type workspaceOverview struct {
	StartTime          time.Time       `json:"start_time"`
	EndTime            time.Time       `json:"end_time"`
	Timezone           string          `json:"timezone"`
	AgentCount         int64           `json:"agent_count"`
	SessionCount       int64           `json:"session_count"`
	FailedSessionCount int64           `json:"failed_session_count"`
	ActiveAgentCount   int             `json:"active_agent_count"`
	Daily              []overviewDay   `json:"daily"`
	Agents             []overviewAgent `json:"agents"`
}

func RegisterWorkspaceOverviewRoute(r chi.Router, store workspaceOverviewStore) {
	r.Get("/workspaces/{workspaceId}/overview", handleWorkspaceOverview(store))
}

func handleWorkspaceOverview(store workspaceOverviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		org, ok := visibleOrgUUID(w, r)
		if !ok {
			return
		}
		scope, ok := consoleWorkspaceScopeFromRequest(w, r, store, org)
		if !ok {
			return
		}
		query, ok := parseOverviewQuery(w, r, time.Now())
		if !ok {
			return
		}
		query.OrganizationUUID, query.WorkspaceUUID = org, scope.UUID
		rows, err := store.AggregateWorkspaceOverview(r.Context(), query)
		if err != nil {
			internalError(w, "failed to load overview")
			return
		}
		count, err := store.CountWorkspaceOverviewAgents(r.Context(), org, scope.UUID)
		if err != nil {
			internalError(w, "failed to count agents")
			return
		}
		report := buildWorkspaceOverview(query, rows)
		report.AgentCount = count
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, report)
	}
}

func parseOverviewQuery(w http.ResponseWriter, r *http.Request, now time.Time) (db.WorkspaceOverviewQuery, bool) {
	days := 30
	switch r.URL.Query().Get("days") {
	case "", "30":
	case "7":
		days = 7
	default:
		invalidOverviewQuery(w)
		return db.WorkspaceOverviewQuery{}, false
	}
	zone := r.URL.Query().Get("timezone")
	if zone == "" {
		zone = "UTC"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil || zone == "Local" {
		invalidOverviewQuery(w)
		return db.WorkspaceOverviewQuery{}, false
	}
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1-days)
	return db.WorkspaceOverviewQuery{Start: start.UTC(), End: now.UTC(), Timezone: zone}, true
}

func buildWorkspaceOverview(query db.WorkspaceOverviewQuery, rows []db.WorkspaceOverviewBucket) workspaceOverview {
	report := workspaceOverview{StartTime: query.Start, EndTime: query.End, Timezone: query.Timezone, Daily: []overviewDay{}, Agents: []overviewAgent{}}
	agents := map[string]overviewAgent{}
	for _, row := range rows {
		report.SessionCount += row.SessionCount
		report.FailedSessionCount += row.FailedSessionCount
		report.Daily = append(report.Daily, overviewDay{Date: row.Date, AgentID: row.AgentID, SessionCount: row.SessionCount})
		agent := agents[row.AgentID]
		agent.AgentID, agent.Name = row.AgentID, row.AgentName
		agent.SessionCount += row.SessionCount
		agent.FailedSessionCount += row.FailedSessionCount
		if row.LastRunAt.After(agent.LastRunAt) {
			agent.LastRunAt = row.LastRunAt
		}
		agents[row.AgentID] = agent
	}
	for _, agent := range agents {
		report.Agents = append(report.Agents, agent)
	}
	slices.SortFunc(report.Agents, func(a, b overviewAgent) int {
		return cmp.Or(cmp.Compare(b.SessionCount, a.SessionCount), cmp.Compare(a.AgentID, b.AgentID))
	})
	report.ActiveAgentCount = len(report.Agents)
	return report
}
