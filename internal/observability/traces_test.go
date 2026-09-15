package observability

import (
	"testing"
	"time"
)

func TestMapTraceListHasMoreAndStatus(t *testing.T) {
	rows := make([]Row, 0, 51)
	start := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 51; i++ {
		rows = append(rows, Row{
			"trace_id":    "trace-" + string(rune('a'+i%26)),
			"session_id":  "sess",
			"agent_id":    "agent_01",
			"start_time":  start.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			"duration_ms": 10.0,
			"tokens":      1.0,
			"llm_calls":   2.0,
			"tool_calls":  3.0,
			"has_error":   float64(i % 2),
		})
	}
	items, hasMore, err := mapTraceList(rows)
	if err != nil {
		t.Fatalf("mapTraceList() error = %v", err)
	}
	if !hasMore || len(items) != 50 {
		t.Fatalf("hasMore=%v len=%d", hasMore, len(items))
	}
	if items[1].Status != "error" || items[0].Status != "ok" {
		t.Fatalf("status = %q %q", items[0].Status, items[1].Status)
	}
	if items[0].AgentID != "agent_01" {
		t.Fatalf("agent_id = %q", items[0].AgentID)
	}
}

func TestMapTraceListItemPreview(t *testing.T) {
	start := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	item, err := mapTraceListItem(Row{
		"trace_id":   "t1",
		"start_time": start.Format(time.RFC3339),
		"input":      "hello",
		"output":     "world",
	})
	if err != nil {
		t.Fatalf("mapTraceListItem() error = %v", err)
	}
	if item.Input != "hello" || item.Output != "world" {
		t.Fatalf("preview = %q %q", item.Input, item.Output)
	}
}

func TestMapTraceListEmpty(t *testing.T) {
	empty, hasMore, err := mapTraceList(nil)
	if err != nil || hasMore || len(empty) != 0 {
		t.Fatalf("empty list items=%v hasMore=%v err=%v", empty, hasMore, err)
	}
}

func TestMapTraceSpansCopiesAttributes(t *testing.T) {
	spans := mapTraceSpans([]Span{{
		SpanID: "s1", Kind: "llm", Name: "claude_code.llm_request", Status: "ok",
		Attributes: map[string]string{"model": "claude-sonnet-4"},
	}})
	if len(spans) != 1 || spans[0].Attributes["model"] != "claude-sonnet-4" {
		t.Fatalf("spans = %#v", spans)
	}
}

func TestNormalizeTraceStatus(t *testing.T) {
	if _, err := normalizeTraceStatus("bad"); err == nil {
		t.Fatal("expected invalid status")
	}
	got, err := normalizeTraceStatus("error")
	if err != nil || got != "error" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
