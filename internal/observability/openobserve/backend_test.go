package openobserve

import (
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

func TestTraceDetailWindowUnbounded(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	start, end := traceDetailWindow(observability.TimeWindow{}, now)
	if !start.Equal(time.Unix(1, 0).UTC()) {
		t.Fatalf("start = %s, want unix 1s", start)
	}
	if start.UnixMicro() == 0 {
		t.Fatal("OpenObserve rejects start_time=0")
	}
	if !end.Equal(now.Add(time.Hour)) {
		t.Fatalf("end = %s, want now+1h", end)
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

func TestTraceDetailWindowKeepsBoundRange(t *testing.T) {
	window := observability.TimeWindow{
		Start: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
	}
	start, end := traceDetailWindow(window, time.Now().UTC())
	if !start.Equal(window.Start) || !end.Equal(window.End) {
		t.Fatalf("got %s %s", start, end)
	}
}
