package openobserve

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

const (
	sizeDefault     = 1000
	sizeTimeseries  = 10000
	sizeTraceList   = 51
	sizeTraceDetail = 2000

	traceListPreviewLimit    = 240
	traceListOutputTimeWidth = 24
)

type Backend struct {
	client *searchClient
}

func New(cfg config.OpenObserveConfig, logger *slog.Logger) *Backend {
	return NewWithHTTPClient(cfg, logger, nil)
}

func NewWithHTTPClient(cfg config.OpenObserveConfig, logger *slog.Logger, httpClient *http.Client) *Backend {
	return &Backend{client: newSearchClient(cfg, logger, httpClient)}
}

func (b *Backend) PanelRows(ctx context.Context, queryRef string, bound observability.BoundVariables) ([]observability.Row, error) {
	query, ok := lookupQuery(queryRef)
	if !ok {
		return nil, observability.QueryInternal("unknown query_ref "+queryRef, nil)
	}
	sql, err := renderSQL(query.SQL, bound, query.StreamType, renderExtras{})
	if err != nil {
		return nil, err
	}
	start, end := searchWindow(queryRef, bound)
	hits, err := b.client.search(ctx, query.StreamType, sql, start, end, sizeFor(queryRef))
	if err != nil {
		return nil, err
	}
	return rowsFromHits(hits), nil
}

func (b *Backend) TraceListRows(ctx context.Context, q observability.TraceListQuery) ([]observability.Row, error) {
	query, ok := lookupQuery("trace.list")
	if !ok {
		return nil, observability.QueryInternal("missing trace.list query", nil)
	}
	filter, err := traceIDFilter(q.TraceID)
	if err != nil {
		return nil, err
	}
	bound := q.Bound
	bound.Offset = q.Offset
	sql, err := renderSQL(query.SQL, bound, query.StreamType, renderExtras{
		traceIDFilter: filter,
		statusHaving:  statusHaving(q.Status),
	})
	if err != nil {
		return nil, err
	}
	hits, err := b.client.search(ctx, query.StreamType, sql, bound.Window.Start, bound.Window.End, sizeTraceList)
	if err != nil {
		return nil, err
	}
	rows := make([]observability.Row, 0, len(hits))
	for _, hit := range hits {
		row, mapErr := normalizeTraceListHit(hit)
		if mapErr != nil {
			return nil, mapErr
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (b *Backend) TraceSpans(ctx context.Context, q observability.TraceDetailQuery) (observability.TraceSpansResult, error) {
	query, ok := lookupQuery("trace.detail")
	if !ok {
		return observability.TraceSpansResult{}, observability.QueryInternal("missing trace.detail query", nil)
	}
	sql, err := renderSQL(query.SQL, q.Bound, query.StreamType, renderExtras{})
	if err != nil {
		return observability.TraceSpansResult{}, err
	}
	hits, err := b.client.search(ctx, query.StreamType, sql, q.Bound.Window.Start, q.Bound.Window.End, sizeTraceDetail+1)
	if err != nil {
		return observability.TraceSpansResult{}, err
	}
	truncated := len(hits) > sizeTraceDetail
	if truncated {
		hits = hits[:sizeTraceDetail]
	}
	spans := make([]observability.Span, 0, len(hits))
	for _, hit := range hits {
		span, mapErr := spanFromHit(hit)
		if mapErr != nil {
			return observability.TraceSpansResult{}, mapErr
		}
		spans = append(spans, span)
	}
	return observability.TraceSpansResult{Spans: spans, Truncated: truncated}, nil
}

func searchWindow(queryRef string, bound observability.BoundVariables) (time.Time, time.Time) {
	renderType, ok := observability.QueryRenderType(queryRef)
	if ok && renderType == "stat" {
		return bound.Window.PrevStart, bound.Window.End
	}
	return bound.Window.Start, bound.Window.End
}

func sizeFor(queryRef string) int {
	renderType, ok := observability.QueryRenderType(queryRef)
	if ok && renderType == "timeseries" {
		return sizeTimeseries
	}
	return sizeDefault
}

func rowsFromHits(hits []map[string]any) []observability.Row {
	rows := make([]observability.Row, 0, len(hits))
	for _, hit := range hits {
		row := observability.Row{}
		for key, value := range hit {
			row[key] = value
		}
		rows = append(rows, row)
	}
	return rows
}

func normalizeTraceListHit(hit map[string]any) (observability.Row, error) {
	start, err := unixNanoTime(hit["start_time"])
	if err != nil {
		return nil, err
	}
	return observability.Row{
		"trace_id":    anyString(hit["trace_id"]),
		"agent_id":    anyString(hit["agent_id"]),
		"session_id":  anyString(hit["session_id"]),
		"start_time":  start,
		"duration_ms": hit["duration_ms"],
		"tokens":      hit["tokens"],
		"llm_calls":   hit["llm_calls"],
		"tool_calls":  hit["tool_calls"],
		"has_error":   hit["has_error"],
		"input":       clipPreview(anyString(hit["input"]), traceListPreviewLimit),
		"output":      decodeTraceListOutput(hit["output"]),
	}, nil
}

func decodeTraceListOutput(raw any) string {
	text := anyString(raw)
	if len(text) >= traceListOutputTimeWidth {
		text = text[traceListOutputTimeWidth:]
	}
	return clipPreview(text, traceListPreviewLimit)
}

func clipPreview(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func anyString(value any) string {
	if value == nil {
		return ""
	}
	if raw, ok := value.(string); ok {
		return raw
	}
	return fmt.Sprint(value)
}
