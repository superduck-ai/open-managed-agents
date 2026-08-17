package openobserve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

func TestTraceSpansReportsTruncation(t *testing.T) {
	var captured searchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode body: %v", err)
		}
		hits := make([]map[string]any, sizeTraceDetail+1)
		for index := range hits {
			hits[index] = map[string]any{"span_id": strconv.Itoa(index), "start_time": strconv.Itoa(index + 1)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"hits": hits})
	}))
	defer server.Close()
	backend := NewWithHTTPClient(config.OpenObserveConfig{
		BaseURL: server.URL, Organization: "oma",
		Query: config.BackendQueryConfig{Username: "u", Password: "p", Timeout: time.Second},
	}, nil, server.Client())
	bound := testBound()
	bound.Values["trace_id"] = observability.TypedValue{Str: "trace-1"}
	result, err := backend.TraceSpans(context.Background(), observability.TraceDetailQuery{Bound: bound, TraceID: "trace-1"})
	if err != nil {
		t.Fatalf("TraceSpans() error = %v", err)
	}
	if !result.Truncated || len(result.Spans) != sizeTraceDetail {
		t.Fatalf("result = truncated %v spans %d", result.Truncated, len(result.Spans))
	}
	if captured.Query.Size != sizeTraceDetail+1 || captured.Query.StartTime != bound.Window.Start.UnixMicro() || captured.Query.EndTime != bound.Window.End.UnixMicro() {
		t.Fatalf("query = %+v", captured.Query)
	}
}

func TestNormalizeTraceListHitPreview(t *testing.T) {
	start := float64(time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC).UnixNano())
	row, err := normalizeTraceListHit(map[string]any{
		"trace_id":   "abc",
		"start_time": start,
		"input":      "hello\nworld",
		"output":     strings.Repeat("0", 24) + "final answer",
	})
	if err != nil {
		t.Fatalf("normalizeTraceListHit() error = %v", err)
	}
	if row["input"] != "hello world" {
		t.Fatalf("input = %v", row["input"])
	}
	if row["output"] != "final answer" {
		t.Fatalf("output = %v", row["output"])
	}
	if output := decodeTraceListOutput(strings.Repeat("0", traceListOutputTimeWidth)); output != "" {
		t.Fatalf("empty output = %q", output)
	}

	empty, err := normalizeTraceListHit(map[string]any{
		"trace_id":   "abc",
		"start_time": start,
	})
	if err != nil {
		t.Fatalf("normalizeTraceListHit() error = %v", err)
	}
	if empty["input"] != "" || empty["output"] != "" {
		t.Fatalf("empty preview = %v %v", empty["input"], empty["output"])
	}
}
