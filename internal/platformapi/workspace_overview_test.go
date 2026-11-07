package platformapi

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type overviewTestStore struct {
	consoleAPIKeyScopeStore
	rows  []db.WorkspaceOverviewBucket
	query db.WorkspaceOverviewQuery
	calls int
	err   error
}

func (s *overviewTestStore) AggregateWorkspaceOverview(_ context.Context, query db.WorkspaceOverviewQuery) ([]db.WorkspaceOverviewBucket, error) {
	s.query = query
	s.calls++
	return s.rows, s.err
}
func (s *overviewTestStore) CountWorkspaceOverviewAgents(context.Context, string, string) (int64, error) {
	return 4, nil
}

type overviewDeniedStore struct{ *overviewTestStore }

func (s overviewDeniedStore) GetAdminUser(_ context.Context, org, user string) (db.AdminUser, error) {
	return db.AdminUser{UUID: "00000000-0000-4000-8000-000000000003", ExternalID: user, OrganizationUUID: org, Role: "user"}, nil
}

func TestWorkspaceOverview(t *testing.T) {
	newStore := func() *overviewTestStore {
		return &overviewTestStore{consoleAPIKeyScopeStore: consoleAPIKeyScopeStore{workspaces: []ConsoleWorkspace{{UUID: "00000000-0000-4000-8000-000000000001", ExternalID: "workspace_test"}}}}
	}
	t.Run("denies organization mismatch before reading data", func(t *testing.T) {
		store := newStore()
		r := consoleWorkspaceAPIKeyTestRequest("workspace_test")
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{OrganizationUUID: "other", UserExternalID: "actor"}))
		w := httptest.NewRecorder()
		handleWorkspaceOverview(store).ServeHTTP(w, r)
		if w.Code == http.StatusOK || store.calls != 0 {
			t.Fatalf("status=%d calls=%d", w.Code, store.calls)
		}
	})
	t.Run("denies a workspace without membership before aggregation", func(t *testing.T) {
		store := newStore()
		w := httptest.NewRecorder()
		handleWorkspaceOverview(overviewDeniedStore{store}).ServeHTTP(w, consoleWorkspaceAPIKeyTestRequest("workspace_test"))
		if w.Code != http.StatusForbidden || store.calls != 0 {
			t.Fatalf("status=%d calls=%d", w.Code, store.calls)
		}
	})

	t.Run("rejects missing workspace", func(t *testing.T) {
		store := newStore()
		w := httptest.NewRecorder()
		handleWorkspaceOverview(store).ServeHTTP(w, consoleWorkspaceAPIKeyTestRequest("missing"))
		if w.Code != http.StatusNotFound || store.calls != 0 {
			t.Fatalf("status=%d calls=%d", w.Code, store.calls)
		}
	})
	t.Run("does not turn query errors into empty data", func(t *testing.T) {
		store := newStore()
		store.err = errors.New("query failed")
		w := httptest.NewRecorder()
		handleWorkspaceOverview(store).ServeHTTP(w, consoleWorkspaceAPIKeyTestRequest("workspace_test"))
		if w.Code != http.StatusInternalServerError {
			t.Fatal(w.Code)
		}
	})
	t.Run("aggregates daily rows consistently within resolved scope", func(t *testing.T) {
		store := newStore()
		at := time.Date(2026, 11, 6, 5, 0, 0, 0, time.UTC)
		store.rows = []db.WorkspaceOverviewBucket{{Date: "2026-11-05", AgentID: "agent_a", AgentName: "A", SessionCount: 2, FailedSessionCount: 1, LastRunAt: at.Add(-24 * time.Hour)}, {Date: "2026-11-06", AgentID: "agent_a", AgentName: "A", SessionCount: 3, LastRunAt: at}, {Date: "2026-11-06", AgentID: "agent_b", AgentName: "B", SessionCount: 1, LastRunAt: at}}
		r := consoleWorkspaceAPIKeyTestRequest("workspace_test")
		r.URL.RawQuery = "days=7&timezone=Asia%2FShanghai"
		w := httptest.NewRecorder()
		handleWorkspaceOverview(store).ServeHTTP(w, r)
		var report workspaceOverview
		if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || report.SessionCount != 6 || report.FailedSessionCount != 1 || report.ActiveAgentCount != 2 || report.AgentCount != 4 || len(report.Daily) != 3 || report.Agents[0].SessionCount != 5 || !report.Agents[0].LastRunAt.Equal(at) {
			t.Fatalf("status=%d report=%+v", w.Code, report)
		}
		if store.query.WorkspaceUUID != store.workspaces[0].UUID || store.query.OrganizationUUID == "" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("scope or cache header missing")
		}
	})
	t.Run("empty results use arrays", func(t *testing.T) {
		w := httptest.NewRecorder()
		handleWorkspaceOverview(newStore()).ServeHTTP(w, consoleWorkspaceAPIKeyTestRequest("workspace_test"))
		var report workspaceOverview
		if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || report.Daily == nil || report.Agents == nil || report.SessionCount != 0 {
			t.Fatalf("%+v", report)
		}
	})
}

func TestOverviewWindow(t *testing.T) {
	for _, raw := range []string{"days=8", "days=-1", "timezone=Invalid%2FZone", "timezone=Local"} {
		r := httptest.NewRequest("GET", "/?"+raw, nil)
		w := httptest.NewRecorder()
		if _, ok := parseOverviewQuery(w, r, time.Now()); ok || w.Code != 400 {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, tc := range []struct {
		zone  string
		now   string
		start string
	}{{"Asia/Shanghai", "2026-11-06T01:00:00Z", "2026-10-30T16:00:00Z"}, {"America/New_York", "2026-11-03T12:00:00Z", "2026-10-28T04:00:00Z"}} {
		now, err := time.Parse(time.RFC3339, tc.now)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", "/?days=7&timezone="+tc.zone, nil)
		q, ok := parseOverviewQuery(httptest.NewRecorder(), r, now)
		if !ok || q.Start.Format(time.RFC3339) != tc.start || !q.End.Equal(now) {
			t.Fatalf("%s: %+v", tc.zone, q)
		}
	}
}
