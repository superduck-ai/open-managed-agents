package observability

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestDashboardCatalogRejectsInconsistentDefinitions(t *testing.T) {
	catalog, err := loadDashboardCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*dashboardCatalog)
	}{
		{"unknown query", func(c *dashboardCatalog) { c.Tabs[0].Panels[0].Query = "missing" }},
		{"unknown render", func(c *dashboardCatalog) { c.Tabs[0].Panels[0].Render = "pie" }},
		{"span out of range", func(c *dashboardCatalog) { c.Tabs[0].Panels[0].Span = 13 }},
		{"compare without compare query", func(c *dashboardCatalog) { c.Tabs[0].Panels[0].Query = "session_trend" }},
		{"query without SQL template", func(c *dashboardCatalog) { c.Queries["orphan"] = querySpec{Size: 1} }},
		{"duplicate tab", func(c *dashboardCatalog) { c.Tabs = append(c.Tabs, c.Tabs[0]) }},
		{"filter without options", func(c *dashboardCatalog) { c.Tabs[1].Options = "" }},
		{"unknown trend query", func(c *dashboardCatalog) { c.Tabs[0].Panels[0].Trend = "missing" }},
		{"lookback without a From bound", func(c *dashboardCatalog) { c.Queries["session_trend"] = querySpec{Size: 1, Lookback: true} }},
		{"trend on a non-stat panel", func(c *dashboardCatalog) {
			index := slices.IndexFunc(c.Tabs[0].Panels, func(panel panelSpec) bool { return panel.Render == "series" })
			c.Tabs[0].Panels[index].Trend = "session_trend"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			copied := &dashboardCatalog{Queries: map[string]querySpec{}, templates: catalog.templates}
			for name, query := range catalog.Queries {
				copied.Queries[name] = query
			}
			for _, tab := range catalog.Tabs {
				tab.Panels = slices.Clone(tab.Panels)
				copied.Tabs = append(copied.Tabs, tab)
			}
			test.mutate(copied)
			if copied.validate() == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}

func TestDashboardQueriesRenderScopedSQL(t *testing.T) {
	catalog, err := dashboards()
	if err != nil {
		t.Fatal(err)
	}
	backend := NewOpenObserve(config.OpenObserveConfig{LogsStream: "oma_dsh"}, nil)
	source, err := backend.scopedQuery(Scope{OrganizationUUID: "org", WorkspaceUUID: "ws", AgentID: "agent_1", AgentVersions: []int{2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	for name := range catalog.Queries {
		for _, params := range []sqlParams{{Source: source, Start: 10, End: 20, Interval: 5}, {Source: source, Start: 10, End: 20, Interval: 5, Model: "m'1", Tool: "Bash"}} {
			sql, err := catalog.render(name, params)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, required := range []string{"oma_organization_uuid = 'org'", "oma_workspace_uuid = 'ws'", "oma_agent_id = 'agent_1'", "CAST(oma_agent_version AS BIGINT) IN (2, 3)"} {
				if !strings.Contains(sql, required) {
					t.Fatalf("%s lost tenant condition %q", name, required)
				}
			}
			if strings.Contains(sql, "{{") || strings.Contains(sql, "m'1") {
				t.Fatalf("%s rendered unsafe SQL: %s", name, sql)
			}
		}
	}
}

func TestDashboardRequestValidation(t *testing.T) {
	service := New(&fakeBackend{}, &fakeStore{})
	scope := Scope{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	for _, test := range []struct {
		request DashboardRequest
		kind    apperr.Kind
	}{
		{DashboardRequest{Tab: "billing"}, apperr.NotFound},
		{DashboardRequest{Tab: "overview"}, apperr.NotFound},
		{DashboardRequest{Tab: "models", Model: "bad model"}, apperr.InvalidArgument},
		{DashboardRequest{Tab: "tools", Tool: strings.Repeat("x", 257)}, apperr.InvalidArgument},
	} {
		var appError *apperr.Error
		requestScope := scope
		if test.request.Tab == "overview" {
			requestScope.SessionID = "sesn_missing"
		}
		if _, err := service.Dashboard(context.Background(), requestScope, Window{Start: time.Unix(0, 0), End: time.Unix(3600, 0)}, test.request); !errors.As(err, &appError) || appError.Kind != test.kind {
			t.Fatalf("%+v error = %v", test.request, err)
		}
	}
	if _, err := ParseAgentVersions([]string{"1", "0"}); err == nil {
		t.Fatal("non-positive version accepted")
	}
	if versions, err := ParseAgentVersions([]string{" 3", "1", "3"}); err != nil || !slices.Equal(versions, []int{1, 3}) {
		t.Fatalf("versions = %v %v", versions, err)
	}
}

func TestDashboardPanelsMapRows(t *testing.T) {
	backend := &fakeBackend{rows: map[string][]Row{
		"sessions_summary":  {{"active_sessions": json.Number("8"), "active_sessions_previous": json.Number("2")}},
		"turns_summary":     {{"interactions": json.Number("5"), "duration_p50": json.Number("5273.0"), "duration_p95": json.Number("36609")}},
		"requests_summary":  {{"tokens": json.Number("1200"), "requests": json.Number("11"), "ttft_avg": json.Number("1491.5")}},
		"turns_per_session": {{"p50": json.Number("1"), "p90": json.Number("2"), "p95": json.Number("3")}},
		"request_trend": {
			{"bucket_start": json.Number("3600000"), "series": "failed", "value": json.Number("1")},
			{"bucket_start": json.Number("0"), "series": "succeeded", "value": json.Number("4")},
		},
		"tool_top": {{"name": "Bash", "value": json.Number("3")}, {"name": "", "value": json.Number("9")}},
		"session_trend": {
			{"bucket_start": json.Number("0"), "series": "sessions", "value": json.Number("3")},
			{"bucket_start": json.Number("240000"), "series": "sessions", "value": json.Number("5")},
		},
		"turn_duration_trend": {
			{"bucket_start": json.Number("120000"), "series": "p95", "value": json.Number("900")},
			{"bucket_start": json.Number("120000"), "series": "p50", "value": json.Number("400")},
		},
	}}
	service := New(backend, &fakeStore{})
	window := Window{Start: time.UnixMilli(0), End: time.UnixMilli(2 * 3600000)}
	dashboard, err := service.Dashboard(context.Background(), Scope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, window, DashboardRequest{Tab: "overview", Model: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if backend.request.Model != "" || slices.Contains(backend.queries, "versions") || !slices.Contains(backend.queries, "session_trend") || dashboard.IntervalMs != 120000 {
		t.Fatalf("request=%+v queries=%v", backend.request, backend.queries)
	}
	panels := map[string]Panel{}
	for _, panel := range dashboard.Panels {
		panels[panel.ID] = panel
	}
	if _, found := panels["versions"]; found {
		t.Fatal("agent-only panel shown in workspace scope")
	}
	active := panels["active_sessions"].Data.(StatValue)
	duration := panels["interaction_duration"].Data.(StatValue)
	if *active.Value != 8 || *active.Previous != 2 || *duration.Secondary != 36609 || duration.Previous != nil {
		t.Fatalf("stats = %+v %+v", active, duration)
	}
	if len(active.Trend) != 60 || *active.Trend[0].V != 3 || *active.Trend[1].V != 0 || *active.Trend[2].V != 5 {
		t.Fatalf("summed trend = %+v", active.Trend[:3])
	}
	if duration.Trend[0].V != nil || *duration.Trend[1].V != 400 {
		t.Fatalf("series trend must keep gaps and only read its series: %+v", duration.Trend[:2])
	}
	if percentiles := panels["turns_per_session"].Data.(Percentiles); *percentiles.P90 != 2 {
		t.Fatalf("percentiles = %+v", percentiles)
	}
	series := panels["model_request_outcomes"].Data.([]Series)
	if len(series) != 2 || series[0].Key != "succeeded" || series[1].Key != "failed" || len(series[0].Points) != 60 || *series[0].Points[0].V != 4 || *series[1].Points[30].V != 1 || *series[1].Points[0].V != 0 {
		t.Fatalf("series = %+v", series)
	}
	if bars := panels["tool_top"].Data.([]Bar); len(bars) != 1 || bars[0].Name != "Bash" {
		t.Fatalf("bars = %+v", bars)
	}
}

func TestDashboardSeriesKeepsTopEntriesAndFoldsOthers(t *testing.T) {
	rows := []Row{}
	for index, name := range []string{"a", "b", "c", "d"} {
		rows = append(rows, Row{"bucket_start": json.Number("0"), "series": name, "value": json.Number(string(rune('1' + index)))})
	}
	window := Window{Start: time.UnixMilli(0), End: time.UnixMilli(3600000)}
	folded := seriesData(panelSpec{Top: 2, Other: true}, rows, window)
	if len(folded) != 3 || folded[0].Key != "d" || folded[1].Key != "c" || folded[2].Key != otherSeries || *folded[2].Points[0].V != 3 {
		t.Fatalf("folded = %+v", folded)
	}
	if dropped := seriesData(panelSpec{Top: 2}, rows, window); len(dropped) != 2 {
		t.Fatalf("dropped = %+v", dropped)
	}
	sparse := seriesData(panelSpec{Chart: "line"}, rows[:1], Window{Start: time.UnixMilli(0), End: time.UnixMilli(2 * 3600000)})
	if sparse[0].Points[0].V == nil || sparse[0].Points[1].V != nil {
		t.Fatalf("line buckets without data must stay empty: %+v", sparse)
	}
}

func TestDashboardIntervalKeepsAtMostOneHundredBuckets(t *testing.T) {
	for _, test := range []struct {
		span     time.Duration
		interval time.Duration
	}{
		{time.Minute, time.Minute},
		{100 * time.Minute, time.Minute},
		{101 * time.Minute, 2 * time.Minute},
		{6 * time.Hour, 5 * time.Minute},
		{24 * time.Hour, 15 * time.Minute},
		{7 * 24 * time.Hour, 3 * time.Hour},
		{30 * 24 * time.Hour, 12 * time.Hour},
		{366 * 24 * time.Hour, 24 * time.Hour},
	} {
		window := Window{Start: time.UnixMilli(0), End: time.UnixMilli(0).Add(test.span)}
		if got := dashboardInterval(window); got != test.interval.Milliseconds() {
			t.Fatalf("span %s interval = %d, want %s", test.span, got, test.interval)
		}
	}
}

func TestDashboardModelFilterAndSubagentNames(t *testing.T) {
	backend := &fakeBackend{rows: map[string][]Row{
		"model_names": {{"name": "model-a"}, {"name": "model-b"}},
		"subagent_threads": {
			{"thread_id": "sthr_1", "turns": json.Number("2"), "failures": json.Number("1"), "total_ms": json.Number("300"), "max_ms": json.Number("200"), "tokens": json.Number("50")},
			{"thread_id": "sthr_2", "turns": json.Number("1"), "total_ms": json.Number("100"), "max_ms": json.Number("100"), "tokens": json.Number("10")},
			{"thread_id": "sthr_unnamed", "turns": json.Number("9")},
			{"thread_id": "sthr_blank", "turns": json.Number("4")},
		},
	}}
	service := New(backend, &fakeStore{threadNames: map[string]string{"sthr_1": "reviewer", "sthr_2": "reviewer", "sthr_blank": ""}})
	scope := Scope{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	window := Window{Start: time.UnixMilli(0), End: time.UnixMilli(3600000)}
	models, err := service.Dashboard(context.Background(), scope, window, DashboardRequest{Tab: "models", Model: "model-a", Tool: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if backend.request.Model != "model-a" || backend.request.Tool != "" || !slices.Equal(models.FilterOptions, []string{"model-a", "model-b"}) {
		t.Fatalf("request=%+v options=%v", backend.request, models.FilterOptions)
	}
	subagents, err := service.Dashboard(context.Background(), scope, window, DashboardRequest{Tab: "subagents"})
	if err != nil {
		t.Fatal(err)
	}
	table := subagents.Panels[1].Data.(TableData)
	unnamed, blank, row := table.Rows[0], table.Rows[1], table.Rows[2]
	if len(table.Rows) != 3 || blank["name"] != "sthr_blank" || blank["delegations"] != 4.0 || unnamed["name"] != "sthr_unnamed" || unnamed["delegations"] != 9.0 || unnamed["avg_ms"].(*float64) == nil {
		t.Fatalf("subagents = %+v", table.Rows)
	}
	if row["name"] != "reviewer" || row["threads"] != 2.0 || row["delegations"] != 3.0 || row["tokens"] != 60.0 || *row["avg_ms"].(*float64) != 400.0/3 {
		t.Fatalf("subagents = %+v", table.Rows)
	}
}

func TestOpenObserveDashboardUsesPreviousWindowForComparisons(t *testing.T) {
	var mutex sync.Mutex
	starts := map[string]int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query searchQuery `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		mutex.Lock()
		starts[strings.Fields(request.Query.SQL)[1]] = request.Query.StartTime
		mutex.Unlock()
		_, _ = w.Write([]byte(`{"hits":[{"active_sessions":9007199254740993}]}`))
	}))
	defer server.Close()
	backend := NewOpenObserve(config.OpenObserveConfig{BaseURL: server.URL, Organization: "oma", LogsStream: "oma_dsh"}, server.Client())
	window := Window{Start: time.UnixMilli(10_000), End: time.UnixMilli(20_000)}
	rows, err := backend.Dashboard(context.Background(), Scope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, window, []string{"sessions_summary", "session_trend"}, DashboardRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if rows["sessions_summary"][0]["active_sessions"] != json.Number("9007199254740993") {
		t.Fatalf("rows = %+v", rows)
	}
	if starts["COUNT(DISTINCT"] != time.UnixMilli(0).UnixMicro() || starts["(CAST(dsh_event_time"] != window.Start.UnixMicro() {
		t.Fatalf("search windows = %v", starts)
	}
}

func TestOpenObserveDashboardLooksBackForTurnsAndToolCallsThatStartBeforeTheWindow(t *testing.T) {
	var mutex sync.Mutex
	searches := map[string]searchQuery{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query searchQuery `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		mutex.Lock()
		for _, marker := range []string{"AS interactions", "'p50' AS series", "name AS series", "'failed' ELSE 'succeeded'"} {
			if strings.Contains(request.Query.SQL, marker) {
				searches[marker] = request.Query
			}
		}
		mutex.Unlock()
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer server.Close()
	backend := NewOpenObserve(config.OpenObserveConfig{BaseURL: server.URL, Organization: "oma", LogsStream: "oma_dsh"}, server.Client())
	start, end := time.UnixMilli(100_000_000), time.UnixMilli(100_300_000)
	queries := []string{"turns_summary", "turn_duration_trend", "tool_call_trend", "request_trend"}
	if _, err := backend.Dashboard(context.Background(), Scope{OrganizationUUID: "org", WorkspaceUUID: "ws"}, Window{Start: start, End: end}, queries, DashboardRequest{}); err != nil {
		t.Fatal(err)
	}
	previousStart := start.Add(-end.Sub(start))
	for _, test := range []struct {
		marker string
		search time.Time
		bound  string
	}{
		{"AS interactions", previousStart.Add(-dashboardLookback), "ended_at >= 99700000"},
		{"'p50' AS series", start.Add(-dashboardLookback), "ended_at >= 100000000"},
		{"name AS series", start.Add(-dashboardLookback), "result_at >= 100000000"},
		{"'failed' ELSE 'succeeded'", start, "ended_at >= 100000000"},
	} {
		search, found := searches[test.marker]
		if !found || search.StartTime != test.search.UnixMicro() || search.EndTime != end.UnixMicro() || !strings.Contains(search.SQL, test.bound) {
			t.Fatalf("%s search = %+v", test.marker, search)
		}
	}
}

func TestDashboardSQLSeparatesUnpositionedRequestsAndCountsEmptyRangesAsZero(t *testing.T) {
	catalog, err := dashboards()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"requests_summary", "model_table"} {
		sql, err := catalog.render(name, sqlParams{Source: " FROM oma_dsh WHERE 1 = 1", Start: 10, From: 5, End: 20, Interval: 5})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "WHEN turn > 0 AND step > 0 THEN") || !strings.Contains(sql, "concat(sid, '#', CAST(seq AS VARCHAR))") || !strings.Contains(sql, "COUNT(DISTINCT attempt_key)") {
			t.Fatalf("%s does not fall back to the event sequence: %s", name, sql)
		}
	}
	summary, err := catalog.render("turns_summary", sqlParams{Source: " FROM oma_dsh WHERE 1 = 1", Start: 10, From: 5, End: 20, Interval: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "COALESCE(SUM(CASE WHEN ended_at >= 10 THEN 1 ELSE 0 END), 0) AS interactions") {
		t.Fatalf("turns_summary does not report zero interactions for an empty range: %s", summary)
	}
}

func TestDashboardSeriesWithoutRowsIsEmptyButReturnedZerosArePlotted(t *testing.T) {
	window := Window{Start: time.UnixMilli(0), End: time.UnixMilli(2 * 3600000)}
	spec := panelSpec{Order: []string{"succeeded", "failed"}}
	if empty := seriesData(spec, nil, window); len(empty) != 0 {
		t.Fatalf("series without rows = %+v", empty)
	}
	zeros := seriesData(spec, []Row{{"bucket_start": json.Number("0"), "series": "failed", "value": json.Number("0")}}, window)
	if len(zeros) != 2 || zeros[0].Key != "succeeded" || len(zeros[1].Points) != 60 || *zeros[1].Points[0].V != 0 {
		t.Fatalf("zero series = %+v", zeros)
	}
}
