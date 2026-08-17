package openobserve

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSpanFromHitKindStatusAndAttributes(t *testing.T) {
	start := time.Date(2026, 8, 12, 10, 20, 30, 123456000, time.UTC).UnixNano()
	end := time.Date(2026, 8, 12, 10, 21, 15, 333456000, time.UTC).UnixNano()
	span, err := spanFromHit(map[string]any{
		"span_id":                       "af01",
		"reference_parent_span_id":      "root",
		"operation_name":                "claude_code.llm_request",
		"start_time":                    float64(start),
		"end_time":                      float64(end),
		"duration":                      float64(45100000),
		"span_status":                   "OK",
		"success":                       "false",
		"user_prompt":                   "hello",
		"oma_organization_uuid":         "org",
		"service_oma_organization_uuid": "org",
		"service_oma_session_id":        "sess_01",
		"trace_id":                      "abc",
		"_timestamp":                    1.0,
	})
	if err != nil {
		t.Fatalf("spanFromHit() error = %v", err)
	}
	if span.Kind != "llm" || span.Status != "error" || span.ParentSpanID != "root" {
		t.Fatalf("span = %+v", span)
	}
	if _, ok := span.Attributes["oma_organization_uuid"]; ok {
		t.Fatalf("tenant attr leaked: %#v", span.Attributes)
	}
	if _, ok := span.Attributes["trace_id"]; ok {
		t.Fatalf("structural attr leaked: %#v", span.Attributes)
	}
	if span.Attributes["service_oma_session_id"] != "sess_01" || span.Attributes["user_prompt"] != "hello" {
		t.Fatalf("attrs = %#v", span.Attributes)
	}
	if span.DurationMs != 45100 {
		t.Fatalf("duration_ms = %v, want duration/1000 fallback", span.DurationMs)
	}
}

func TestSpanEventsParsesFlattenedOpenObserveShape(t *testing.T) {
	start := time.Date(2026, 8, 12, 10, 20, 30, 0, time.UTC).UnixNano()
	span, err := spanFromHit(map[string]any{
		"span_id":        "s",
		"operation_name": "claude_code.tool",
		"start_time":     float64(start),
		"end_time":       float64(start + int64(time.Second)),
		"duration_ms":    1.0,
		"events":         `[{"name":"tool.output","_timestamp":1786677194775686807,"output":"file contents"},{"name":"gen_ai.request.attempt","_timestamp":1786677194775686807,"attempt":"1"}]`,
	})
	if err != nil {
		t.Fatalf("spanFromHit() error = %v", err)
	}
	if len(span.Events) != 2 {
		t.Fatalf("events = %+v, want 2 entries", span.Events)
	}
	first := span.Events[0]
	if first.Name != "tool.output" || first.Attributes["output"] != "file contents" {
		t.Fatalf("first event = %+v", first)
	}
	if want := time.Unix(0, 1786677194775686807).UTC(); !first.Timestamp.Equal(want) {
		t.Fatalf("event timestamp = %v, want %v", first.Timestamp, want)
	}
	if _, ok := span.Attributes["events"]; ok {
		t.Fatalf("events column leaked into attributes: %#v", span.Attributes)
	}
}

func TestSpanEventsTruncatesOversizedValuesAndSkipsInvalidJSON(t *testing.T) {
	start := time.Now().UTC().UnixNano()
	oversized := strings.Repeat("界", spanEventValueLimit)
	span, err := spanFromHit(map[string]any{
		"span_id":        "s",
		"operation_name": "claude_code.tool",
		"start_time":     float64(start),
		"end_time":       float64(start + int64(time.Second)),
		"duration_ms":    1.0,
		"events":         `[{"name":"tool.output","_timestamp":1,"output":"` + oversized + `"}]`,
	})
	if err != nil {
		t.Fatalf("spanFromHit() error = %v", err)
	}
	output := span.Events[0].Attributes["output"]
	if len(output) > spanEventValueLimit+len("… [truncated]") || !strings.HasSuffix(output, "… [truncated]") {
		t.Fatalf("output len = %d, want truncated with marker", len(output))
	}
	if !utf8.ValidString(output) {
		t.Fatal("truncated output is not valid UTF-8")
	}

	invalid, err := spanFromHit(map[string]any{
		"span_id":        "s",
		"operation_name": "claude_code.tool",
		"start_time":     float64(start),
		"end_time":       float64(start + int64(time.Second)),
		"duration_ms":    1.0,
		"events":         "not json",
	})
	if err != nil {
		t.Fatalf("spanFromHit() error = %v", err)
	}
	if invalid.Events != nil {
		t.Fatalf("events = %+v, want nil for invalid JSON", invalid.Events)
	}
}

func TestSpanDurationPrefersDurationMs(t *testing.T) {
	start := time.Now().UTC().UnixNano()
	span, err := spanFromHit(map[string]any{
		"span_id":        "s",
		"operation_name": "claude_code.interaction",
		"start_time":     float64(start),
		"end_time":       float64(start + int64(time.Second)),
		"duration_ms":    12.5,
	})
	if err != nil {
		t.Fatalf("spanFromHit() error = %v", err)
	}
	if span.Kind != "interaction" || span.DurationMs != 12.5 || span.Status != "ok" {
		t.Fatalf("span = %+v", span)
	}
}
