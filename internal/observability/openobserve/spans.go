package openobserve

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

var structuralSpanColumns = map[string]struct{}{
	"_timestamp": {}, "start_time": {}, "end_time": {}, "duration": {}, "duration_ms": {},
	"trace_id": {}, "span_id": {}, "parent_span_id": {}, "reference_parent_span_id": {},
	"operation_name": {}, "span_status": {}, "span_kind": {}, "events": {}, "links": {},
	"service_name": {}, "dropped_attributes_count": {}, "dropped_events_count": {},
	"dropped_links_count": {},
}

var tenantStripColumns = map[string]struct{}{
	"oma_organization_uuid":         {},
	"oma_workspace_uuid":            {},
	"service_oma_organization_uuid": {},
	"service_oma_workspace_uuid":    {},
}

func spanFromHit(hit map[string]any) (observability.Span, error) {
	start, err := unixNanoTime(hit["start_time"])
	if err != nil {
		return observability.Span{}, err
	}
	end, err := unixNanoTime(hit["end_time"])
	if err != nil {
		end = start
	}
	durationMs, err := spanDurationMs(hit, start, end)
	if err != nil {
		return observability.Span{}, err
	}
	name := anyString(hit["operation_name"])
	return observability.Span{
		SpanID:       anyString(hit["span_id"]),
		ParentSpanID: parentSpanID(hit),
		Kind:         spanKind(name),
		Name:         name,
		StartTime:    start,
		EndTime:      end,
		DurationMs:   durationMs,
		Status:       spanStatus(hit),
		Attributes:   spanAttributes(hit),
		Events:       spanEvents(hit),
	}, nil
}

// spanEventValueLimit caps a single event attribute value. Tool input/output
// bodies can reach megabytes; the trace detail API returns whole traces, so
// oversized values are truncated at the adapter instead of the transport.
const spanEventValueLimit = 8 << 10

// spanEvents parses the OpenObserve events column: a JSON-encoded array of
// objects with "name", "_timestamp" (unix nanos), and flattened attribute keys.
func spanEvents(hit map[string]any) []observability.SpanEvent {
	raw := strings.TrimSpace(anyString(hit["events"]))
	if raw == "" || raw == "[]" {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber() // float64 无法无损承载纳秒时间戳。
	var entries []map[string]any
	if err := decoder.Decode(&entries); err != nil {
		return nil
	}
	events := make([]observability.SpanEvent, 0, len(entries))
	for _, entry := range entries {
		event := observability.SpanEvent{
			Name:       anyString(entry["name"]),
			Attributes: map[string]string{},
		}
		if number, ok := entry["_timestamp"].(json.Number); ok {
			if nanos, err := number.Int64(); err == nil {
				event.Timestamp = time.Unix(0, nanos).UTC()
			}
		}
		for key, value := range entry {
			if key == "name" || key == "_timestamp" || value == nil {
				continue
			}
			text := strings.TrimSpace(anyString(value))
			if text == "" {
				continue
			}
			if len(text) > spanEventValueLimit {
				cut := spanEventValueLimit
				for cut > 0 && !utf8.RuneStart(text[cut]) {
					cut--
				}
				text = text[:cut] + "… [truncated]"
			}
			event.Attributes[key] = text
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil
	}
	return events
}

func parentSpanID(hit map[string]any) string {
	if value := anyString(hit["reference_parent_span_id"]); value != "" {
		return value
	}
	return anyString(hit["parent_span_id"])
}

func spanKind(operationName string) string {
	switch operationName {
	case "claude_code.interaction":
		return "interaction"
	case "claude_code.llm_request":
		return "llm"
	case "claude_code.tool":
		return "tool"
	case "claude_code.tool.blocked_on_user":
		return "tool_wait"
	case "claude_code.tool.execution":
		return "tool_execution"
	default:
		if strings.HasPrefix(operationName, "claude_code.hook") {
			return "hook"
		}
		return "other"
	}
}

func spanStatus(hit map[string]any) string {
	if strings.EqualFold(anyString(hit["span_status"]), "ERROR") {
		return "error"
	}
	if anyString(hit["success"]) == "false" {
		return "error"
	}
	return "ok"
}

func spanDurationMs(hit map[string]any, start, end time.Time) (float64, error) {
	if raw, ok := hit["duration_ms"]; ok && raw != nil && anyString(raw) != "" {
		value, err := anyFloat(raw)
		if err == nil {
			return value, nil
		}
	}
	if raw, ok := hit["duration"]; ok && raw != nil && anyString(raw) != "" {
		micros, err := anyFloat(raw)
		if err != nil {
			return 0, err
		}
		return micros / 1000, nil
	}
	if end.After(start) {
		return float64(end.Sub(start).Microseconds()) / 1000, nil
	}
	return 0, nil
}

func spanAttributes(hit map[string]any) map[string]string {
	attrs := map[string]string{}
	for key, value := range hit {
		if value == nil {
			continue
		}
		if _, skip := structuralSpanColumns[key]; skip {
			continue
		}
		if _, skip := tenantStripColumns[key]; skip {
			continue
		}
		text := strings.TrimSpace(anyString(value))
		if text == "" {
			continue
		}
		attrs[key] = text
	}
	return attrs
}

func unixNanoTime(value any) (time.Time, error) {
	if value == nil {
		return time.Time{}, observability.QueryInternal("missing span timestamp", nil)
	}
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return parsed.UTC(), nil
		}
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed.UTC(), nil
		}
		nanos, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return time.Time{}, observability.QueryInternal("span timestamp is invalid", err)
		}
		return time.Unix(0, nanos).UTC(), nil
	default:
		nanos, err := anyFloat(value)
		if err != nil {
			return time.Time{}, observability.QueryInternal("span timestamp is invalid", err)
		}
		return time.Unix(0, int64(nanos)).UTC(), nil
	}
}

func anyFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case json.Number:
		value, err := typed.Float64()
		if err != nil {
			return 0, err
		}
		return value, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(typed), 64)
	default:
		parsed, err := strconv.ParseFloat(fmt.Sprint(typed), 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	}
}
