package observability

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strconv"
)

const otherSeries = "other"

var dashboardFilterPattern = regexp.MustCompile(`^[\x21-\x7e]{1,256}$`)

type Panel struct {
	ID      string   `json:"id"`
	Render  string   `json:"render"`
	Unit    string   `json:"unit,omitempty"`
	Span    int      `json:"span"`
	Chart   string   `json:"chart,omitempty"`
	Stacked bool     `json:"stacked,omitempty"`
	Compare bool     `json:"compare,omitempty"`
	Columns []Column `json:"columns,omitempty"`
	Data    any      `json:"data"`
}

type Dashboard struct {
	Tab           string   `json:"tab"`
	IntervalMs    int64    `json:"interval_ms"`
	FilterOptions []string `json:"filter_options"`
	Panels        []Panel  `json:"panels"`
}

type StatValue struct {
	Value     *float64      `json:"value"`
	Previous  *float64      `json:"previous,omitempty"`
	Secondary *float64      `json:"secondary,omitempty"`
	Trend     []SeriesPoint `json:"trend,omitempty"`
}

type Percentiles struct {
	P50 *float64 `json:"p50"`
	P90 *float64 `json:"p90"`
	P95 *float64 `json:"p95"`
}

type SeriesPoint struct {
	T int64    `json:"t"`
	V *float64 `json:"v"`
}

type Series struct {
	Key    string        `json:"key"`
	Points []SeriesPoint `json:"points"`
}

type Bar struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type TableData struct {
	Rows []map[string]any `json:"rows"`
}

func (s *Service) Dashboard(ctx context.Context, scope Scope, window Window, request DashboardRequest) (Dashboard, error) {
	catalog, err := dashboards()
	if err != nil {
		return Dashboard{}, invalidDashboard(err)
	}
	tab, found := catalog.tab(request.Tab)
	if !found {
		return Dashboard{}, UnknownDashboard()
	}
	request, err = dashboardFilters(tab, request)
	if err != nil {
		return Dashboard{}, err
	}
	if err := s.authorizeFilters(ctx, scope); err != nil {
		return Dashboard{}, err
	}
	panels := slices.DeleteFunc(slices.Clone(tab.Panels), func(panel panelSpec) bool {
		return panel.Requires == requireAgent && scope.AgentID == ""
	})
	queries := []string{}
	for _, panel := range panels {
		queries = append(queries, panel.Query)
		if panel.Trend != "" {
			queries = append(queries, panel.Trend)
		}
	}
	if tab.Options != "" {
		queries = append(queries, tab.Options)
	}
	queries = slices.Compact(slices.Sorted(slices.Values(queries)))
	rows, err := s.backend.Dashboard(ctx, scope, window, queries, request)
	if err != nil {
		return Dashboard{}, err
	}
	result := Dashboard{Tab: tab.ID, IntervalMs: dashboardInterval(window), FilterOptions: optionNames(rows[tab.Options]), Panels: make([]Panel, 0, len(panels))}
	for _, spec := range panels {
		data, err := s.panelData(ctx, scope, window, spec, rows)
		if err != nil {
			return Dashboard{}, err
		}
		result.Panels = append(result.Panels, Panel{ID: spec.ID, Render: spec.Render, Unit: spec.Unit, Span: spec.Span, Chart: spec.Chart, Stacked: spec.Stacked, Compare: spec.Compare, Columns: spec.Columns, Data: data})
	}
	return result, nil
}

func dashboardFilters(tab tabSpec, request DashboardRequest) (DashboardRequest, error) {
	filtered := DashboardRequest{Tab: request.Tab}
	switch tab.Filter {
	case filterModel:
		filtered.Model = request.Model
	case filterTool:
		filtered.Tool = request.Tool
	}
	for _, value := range []string{filtered.Model, filtered.Tool} {
		if value != "" && !dashboardFilterPattern.MatchString(value) {
			return DashboardRequest{}, InvalidDashboardFilter()
		}
	}
	return filtered, nil
}

func (s *Service) panelData(ctx context.Context, scope Scope, window Window, spec panelSpec, queryRows map[string][]Row) (any, error) {
	rows := queryRows[spec.Query]
	switch spec.Render {
	case "stat":
		stat := statValue(spec, rows)
		if spec.Trend != "" {
			stat.Trend = trendPoints(queryRows[spec.Trend], spec.TrendSeries, window)
		}
		return stat, nil
	case "percentiles":
		first := firstRow(rows)
		return Percentiles{P50: number(first["p50"]), P90: number(first["p90"]), P95: number(first["p95"])}, nil
	case "series":
		return seriesData(spec, rows, window), nil
	case "bars":
		return bars(rows), nil
	case "subagents":
		return s.subagentTable(ctx, scope.WorkspaceUUID, rows)
	default:
		return tableData(spec.Columns, rows), nil
	}
}

func firstRow(rows []Row) Row {
	if len(rows) == 0 {
		return Row{}
	}
	return rows[0]
}

func number(value any) *float64 {
	var raw string
	switch typed := value.(type) {
	case json.Number:
		raw = typed.String()
	case string:
		raw = typed
	case float64:
		return &typed
	default:
		return nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func numberOr(value any) float64 {
	if parsed := number(value); parsed != nil {
		return *parsed
	}
	return 0
}

func text(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func statValue(spec panelSpec, rows []Row) StatValue {
	first := firstRow(rows)
	stat := StatValue{Value: number(first[spec.Field])}
	if spec.Compare {
		stat.Previous = number(first[spec.Field+"_previous"])
	}
	if spec.Secondary != "" {
		stat.Secondary = number(first[spec.Secondary])
	}
	return stat
}

func seriesData(spec panelSpec, rows []Row, window Window) []Series {
	values := map[string]map[int64]float64{}
	totals := map[string]float64{}
	for _, key := range spec.Order {
		values[key] = map[int64]float64{}
	}
	for _, row := range rows {
		key, bucket, value := text(row["series"]), number(row["bucket_start"]), number(row["value"])
		if key == "" || bucket == nil || value == nil {
			continue
		}
		if values[key] == nil {
			values[key] = map[int64]float64{}
		}
		values[key][int64(*bucket)] += *value
		totals[key] += *value
	}
	if len(totals) == 0 {
		return []Series{}
	}
	keys := orderedSeries(spec, totals, values)
	if spec.Top > 0 && len(keys) > spec.Top {
		kept, rest := keys[:spec.Top], keys[spec.Top:]
		if spec.Other {
			other := map[int64]float64{}
			for _, key := range rest {
				for bucket, value := range values[key] {
					other[bucket] += value
				}
			}
			values[otherSeries] = other
			kept = append(kept, otherSeries)
		}
		keys = kept
	}
	series := make([]Series, 0, len(keys))
	for _, key := range keys {
		series = append(series, Series{Key: key, Points: bucketPoints(values[key], window, spec.Chart == "line")})
	}
	return series
}

func bucketPoints(values map[int64]float64, window Window, gaps bool) []SeriesPoint {
	interval := dashboardInterval(window)
	points := []SeriesPoint{}
	for bucket := window.Start.UnixMilli() / interval * interval; bucket < window.End.UnixMilli(); bucket += interval {
		value, found := values[bucket]
		if !found && gaps {
			points = append(points, SeriesPoint{T: bucket})
			continue
		}
		points = append(points, SeriesPoint{T: bucket, V: &value})
	}
	return points
}

func trendPoints(rows []Row, series string, window Window) []SeriesPoint {
	values := map[int64]float64{}
	for _, row := range rows {
		bucket, value := number(row["bucket_start"]), number(row["value"])
		if bucket == nil || value == nil || (series != "" && text(row["series"]) != series) {
			continue
		}
		values[int64(*bucket)] += *value
	}
	return bucketPoints(values, window, series != "")
}

func orderedSeries(spec panelSpec, totals map[string]float64, values map[string]map[int64]float64) []string {
	keys := slices.Clone(spec.Order)
	rest := []string{}
	for key := range values {
		if !slices.Contains(keys, key) {
			rest = append(rest, key)
		}
	}
	slices.SortFunc(rest, func(left, right string) int {
		return cmp.Or(cmp.Compare(totals[right], totals[left]), cmp.Compare(left, right))
	})
	return append(keys, rest...)
}

func bars(rows []Row) []Bar {
	result := make([]Bar, 0, len(rows))
	for _, row := range rows {
		if name := text(row["name"]); name != "" {
			result = append(result, Bar{Name: name, Value: numberOr(row["value"])})
		}
	}
	return result
}

func tableData(columns []Column, rows []Row) TableData {
	table := TableData{Rows: make([]map[string]any, 0, len(rows))}
	for _, row := range rows {
		projected := make(map[string]any, len(columns))
		for _, column := range columns {
			if column.Unit == "text" {
				projected[column.Key] = text(row[column.Key])
				continue
			}
			projected[column.Key] = number(row[column.Key])
		}
		table.Rows = append(table.Rows, projected)
	}
	return table
}

func optionNames(rows []Row) []string {
	names := []string{}
	for _, row := range rows {
		if name := text(row["name"]); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func (s *Service) subagentTable(ctx context.Context, workspaceUUID string, rows []Row) (TableData, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, text(row["thread_id"]))
	}
	names, err := s.store.ListSubagentThreadNames(ctx, workspaceUUID, ids)
	if err != nil {
		return TableData{}, unavailable(err)
	}
	type subagent struct {
		name                                                   string
		threads, delegations, failures, totalMs, maxMs, tokens float64
	}
	byName := map[string]*subagent{}
	for _, row := range rows {
		threadID := text(row["thread_id"])
		name, found := names[threadID]
		if !found || name == "" {
			name = threadID
		}
		entry := byName[name]
		if entry == nil {
			entry = &subagent{name: name}
			byName[name] = entry
		}
		entry.threads++
		entry.delegations += numberOr(row["turns"])
		entry.failures += numberOr(row["failures"])
		entry.totalMs += numberOr(row["total_ms"])
		entry.maxMs = max(entry.maxMs, numberOr(row["max_ms"]))
		entry.tokens += numberOr(row["tokens"])
	}
	entries := slices.Collect(maps.Values(byName))
	slices.SortFunc(entries, func(left, right *subagent) int {
		return cmp.Or(cmp.Compare(right.delegations, left.delegations), cmp.Compare(left.name, right.name))
	})
	table := TableData{Rows: make([]map[string]any, 0, len(entries))}
	for _, entry := range entries {
		var average *float64
		if entry.delegations > 0 {
			value := entry.totalMs / entry.delegations
			average = &value
		}
		table.Rows = append(table.Rows, map[string]any{"name": entry.name, "threads": entry.threads, "delegations": entry.delegations, "failures": entry.failures, "avg_ms": average, "max_ms": entry.maxMs, "tokens": entry.tokens})
	}
	return table, nil
}
