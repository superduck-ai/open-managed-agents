package openobserve

import (
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

func testBound() observability.BoundVariables {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	return observability.BoundVariables{
		Window:         observability.TimeWindow{Start: start, End: end, PrevStart: start.Add(-24 * time.Hour)},
		BucketInterval: 30 * time.Minute,
		Scope: observability.TenantScope{
			OrganizationUUID: "org-1",
			WorkspaceUUID:    "ws-1",
		},
		Values: map[string]observability.TypedValue{},
	}
}

func TestRenderSQLQuotesAndScope(t *testing.T) {
	bound := testBound()
	bound.Scope.AgentID = "agent_01"
	bound.Scope.AgentVersions = []int64{3, 4}
	bound.Values["model"] = observability.TypedValue{List: []string{"claude-sonnet-4", "o'model"}}
	sql, err := renderSQL(
		"SELECT 1 FROM oma_claude_code WHERE {scope} AND {model_filter} AND _timestamp >= {start_us} AND histogram(_timestamp, '{bucket_interval}') IS NOT NULL",
		bound,
		streamTraces,
		renderExtras{},
	)
	if err != nil {
		t.Fatalf("renderSQL() error = %v", err)
	}
	if !strings.Contains(sql, "service_oma_organization_uuid = 'org-1'") {
		t.Fatalf("missing traces org column: %s", sql)
	}
	if !strings.Contains(sql, "service_oma_agent_id = 'agent_01'") {
		t.Fatalf("missing agent column: %s", sql)
	}
	if !strings.Contains(sql, "service_oma_agent_version IN (3,4)") {
		t.Fatalf("missing agent_version filter: %s", sql)
	}
	if !strings.Contains(sql, "model IN ('claude-sonnet-4','o''model')") {
		t.Fatalf("model filter = %s", sql)
	}
	if strings.Contains(sql, "{") {
		t.Fatalf("leftover placeholder: %s", sql)
	}
	if !strings.Contains(sql, "'30 minutes'") {
		t.Fatalf("bucket = %s", sql)
	}
}

func TestRenderSQLMetricsScopeAndAlias(t *testing.T) {
	bound := testBound()
	bound.Scope.AgentVersions = []int64{7}
	sql, err := renderSQL(
		"SELECT 1 FROM s p JOIN s e ON 1=1 WHERE {scope:p} AND {scope:e} AND {tool_filter:p}",
		bound,
		streamMetrics,
		renderExtras{},
	)
	if err != nil {
		t.Fatalf("renderSQL() error = %v", err)
	}
	if !strings.Contains(sql, "p.oma_organization_uuid = 'org-1'") || !strings.Contains(sql, "e.oma_workspace_uuid = 'ws-1'") {
		t.Fatalf("alias scope = %s", sql)
	}
	if !strings.Contains(sql, "p.oma_agent_version IN (7)") || !strings.Contains(sql, "e.oma_agent_version IN (7)") {
		t.Fatalf("alias agent_version = %s", sql)
	}
	if !strings.Contains(sql, "p.tool_name IN") && !strings.Contains(sql, "1=1") {
		t.Fatalf("tool filter = %s", sql)
	}
}

func TestRenderSQLRejectsMissingAndLeftoverPlaceholders(t *testing.T) {
	bound := testBound()
	if _, err := renderSQL("SELECT 1 FROM oma_claude_code WHERE 1=1", bound, streamTraces, renderExtras{}); err == nil {
		t.Fatal("expected missing scope")
	}
	if _, err := renderSQL("SELECT 1 FROM oma_claude_code WHERE {scope} AND col = {unknown}", bound, streamTraces, renderExtras{}); err == nil {
		t.Fatal("expected leftover placeholder")
	}
}

func TestRenderSQLRejectsInvalidScopeValue(t *testing.T) {
	bound := testBound()
	bound.Scope.AgentID = "agent\n01"
	if _, err := renderSQL("SELECT 1 FROM oma_claude_code WHERE {scope}", bound, streamTraces, renderExtras{}); err == nil {
		t.Fatal("expected tenant scope literal rejection")
	}
}

func TestPercentileQueriesAlignTimestampTypes(t *testing.T) {
	for _, ref := range []string{"overview.session_turns_percentiles", "overview.session_tokens_percentiles"} {
		query, ok := lookupQuery(ref)
		if !ok {
			t.Fatalf("missing %s", ref)
		}
		if strings.Contains(query.SQL, "'' AS timestamp") {
			t.Fatalf("%s uses empty-string timestamp; OpenObserve UNION with histogram() rejects parsing ''", ref)
		}
		if !strings.Contains(query.SQL, "CAST(NULL AS TIMESTAMP) AS timestamp") {
			t.Fatalf("%s missing typed null timestamp for window rows", ref)
		}
		if strings.Contains(query.SQL, "histogram(_timestamp, '{bucket_interval}') AS timestamp") {
			t.Fatalf("%s aliases histogram() as timestamp; UNION with CAST(NULL AS TIMESTAMP) AS timestamp drops window values", ref)
		}
		if !strings.Contains(query.SQL, "histogram(_timestamp, '{bucket_interval}') AS bucket_ts") {
			t.Fatalf("%s sparkline histogram() must alias as bucket_ts before selecting timestamp", ref)
		}
	}
}

func TestQuoteStringEscapesAndRejectsControlChars(t *testing.T) {
	got, err := quoteString("o'reilly")
	if err != nil || got != "'o''reilly'" {
		t.Fatalf("quoteString() = %q err=%v", got, err)
	}
	if _, err := quoteString("bad\nvalue"); err == nil {
		t.Fatal("expected control character rejection")
	}
}

func TestTraceTrendErrorsCountsFailedTraces(t *testing.T) {
	query, ok := packedQueries["trace.trend.errors"]
	if !ok {
		t.Fatal("missing trace.trend.errors")
	}
	if query.StreamType != streamTraces {
		t.Fatalf("stream_type = %q, want traces", query.StreamType)
	}
	if !strings.Contains(query.SQL, "success = 'false'") {
		t.Fatalf("error trend SQL missing success filter: %s", query.SQL)
	}
	if !strings.Contains(query.SQL, "COUNT(DISTINCT CASE") {
		t.Fatalf("error trend SQL should count distinct error traces: %s", query.SQL)
	}
	if strings.Contains(query.SQL, "input_tokens") {
		t.Fatalf("error trend SQL still counts tokens: %s", query.SQL)
	}
}

func TestTraceFilters(t *testing.T) {
	filter, err := traceIDFilter("abc'd")
	if err != nil || filter != "trace_id = 'abc''d'" {
		t.Fatalf("filter = %q err=%v", filter, err)
	}
	if got := statusHaving("error"); got != "HAVING has_error = 1" {
		t.Fatalf("statusHaving error = %q", got)
	}
	if got := statusHaving(""); got != "" {
		t.Fatalf("statusHaving empty = %q", got)
	}
}

func TestTraceListSQLIncludesPreview(t *testing.T) {
	query, ok := lookupQuery("trace.list")
	if !ok {
		t.Fatal("missing trace.list")
	}
	if !strings.Contains(query.SQL, "user_prompt") || !strings.Contains(query.SQL, "AS input") {
		t.Fatalf("trace.list missing input preview: %s", query.SQL)
	}
	if !strings.Contains(query.SQL, "response_model_output") || !strings.Contains(query.SQL, "AS output") {
		t.Fatalf("trace.list missing output preview: %s", query.SQL)
	}
}
