package observability

import (
	"fmt"
	"time"
)

const tracePageSize = 50

type TraceListItem struct {
	TraceID    string    `json:"trace_id"`
	AgentID    string    `json:"agent_id"`
	SessionID  string    `json:"session_id"`
	StartTime  time.Time `json:"start_time"`
	DurationMs float64   `json:"duration_ms"`
	Tokens     float64   `json:"tokens"`
	LLMCalls   float64   `json:"llm_calls"`
	ToolCalls  float64   `json:"tool_calls"`
	Input      string    `json:"input"`
	Output     string    `json:"output"`
	Status     string    `json:"status"`
}

type TraceListResult struct {
	DataAsOf time.Time       `json:"data_as_of"`
	HasMore  bool            `json:"has_more"`
	Items    []TraceListItem `json:"items"`
}

type TraceSpanDTO struct {
	SpanID       string              `json:"span_id"`
	ParentSpanID string              `json:"parent_span_id"`
	Kind         string              `json:"kind"`
	Name         string              `json:"name"`
	StartTime    time.Time           `json:"start_time"`
	EndTime      time.Time           `json:"end_time"`
	DurationMs   float64             `json:"duration_ms"`
	Status       string              `json:"status"`
	Attributes   map[string]string   `json:"attributes"`
	Events       []TraceSpanEventDTO `json:"events"`
}

type TraceSpanEventDTO struct {
	Name       string            `json:"name"`
	Timestamp  time.Time         `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

type TraceDetailResult struct {
	TraceID   string         `json:"trace_id"`
	DataAsOf  time.Time      `json:"data_as_of"`
	Spans     []TraceSpanDTO `json:"spans"`
	Truncated bool           `json:"truncated"`
}

func mapTraceList(rows []Row) (items []TraceListItem, hasMore bool, err error) {
	hasMore = len(rows) > tracePageSize
	if hasMore {
		rows = rows[:tracePageSize]
	}
	items = make([]TraceListItem, 0, len(rows))
	for _, row := range rows {
		item, mapErr := mapTraceListItem(row)
		if mapErr != nil {
			return nil, false, mapErr
		}
		items = append(items, item)
	}
	return items, hasMore, nil
}

func mapTraceListItem(row Row) (TraceListItem, error) {
	traceID, err := stringColumn(row, "trace_id")
	if err != nil {
		return TraceListItem{}, err
	}
	sessionID, err := optionalStringColumn(row, "session_id")
	if err != nil {
		return TraceListItem{}, err
	}
	agentID, err := optionalStringColumn(row, "agent_id")
	if err != nil {
		return TraceListItem{}, err
	}
	startTime, err := timeColumn(row, "start_time")
	if err != nil {
		return TraceListItem{}, err
	}
	durationMs, err := optionalNumeric(row, "duration_ms")
	if err != nil {
		return TraceListItem{}, err
	}
	tokens, err := optionalNumeric(row, "tokens")
	if err != nil {
		return TraceListItem{}, err
	}
	llmCalls, err := optionalNumeric(row, "llm_calls")
	if err != nil {
		return TraceListItem{}, err
	}
	toolCalls, err := optionalNumeric(row, "tool_calls")
	if err != nil {
		return TraceListItem{}, err
	}
	hasError, err := optionalNumeric(row, "has_error")
	if err != nil {
		return TraceListItem{}, err
	}
	input, err := optionalStringColumn(row, "input")
	if err != nil {
		return TraceListItem{}, err
	}
	output, err := optionalStringColumn(row, "output")
	if err != nil {
		return TraceListItem{}, err
	}
	status := "ok"
	if hasError >= 1 {
		status = "error"
	}
	return TraceListItem{
		TraceID:    traceID,
		AgentID:    agentID,
		SessionID:  sessionID,
		StartTime:  startTime,
		DurationMs: durationMs,
		Tokens:     tokens,
		LLMCalls:   llmCalls,
		ToolCalls:  toolCalls,
		Input:      input,
		Output:     output,
		Status:     status,
	}, nil
}

func mapTraceSpans(spans []Span) []TraceSpanDTO {
	out := make([]TraceSpanDTO, 0, len(spans))
	for _, span := range spans {
		attrs := span.Attributes
		if attrs == nil {
			attrs = map[string]string{}
		}
		events := make([]TraceSpanEventDTO, 0, len(span.Events))
		for _, event := range span.Events {
			events = append(events, TraceSpanEventDTO{
				Name:       event.Name,
				Timestamp:  event.Timestamp,
				Attributes: event.Attributes,
			})
		}
		out = append(out, TraceSpanDTO{
			SpanID:       span.SpanID,
			ParentSpanID: span.ParentSpanID,
			Kind:         span.Kind,
			Name:         span.Name,
			StartTime:    span.StartTime,
			EndTime:      span.EndTime,
			DurationMs:   span.DurationMs,
			Status:       span.Status,
			Attributes:   attrs,
			Events:       events,
		})
	}
	return out
}

func optionalStringColumn(row Row, key string) (string, error) {
	raw, ok := row[key]
	if !ok || raw == nil {
		return "", nil
	}
	switch typed := raw.(type) {
	case string:
		return typed, nil
	default:
		return fmt.Sprint(typed), nil
	}
}

func optionalNumeric(row Row, key string) (float64, error) {
	value, ok, err := optionalFloatColumn(row, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return value, nil
}

func normalizeTraceStatus(status string) (string, error) {
	switch status {
	case "", "ok", "error":
		return status, nil
	default:
		return "", errInvalidStatus()
	}
}
