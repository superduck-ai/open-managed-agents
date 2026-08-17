package observability

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type StatData struct {
	Current       *float64 `json:"current"`
	Previous      *float64 `json:"previous"`
	ChangePercent *float64 `json:"change_percent"`
}

type TimeseriesData struct {
	Series []TimeseriesSeries `json:"series"`
}

type TimeseriesSeries struct {
	Name   string            `json:"name"`
	Points []TimeseriesPoint `json:"points"`
}

type TimeseriesPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type CategoricalData struct {
	Items []CategoricalItem `json:"items"`
}

type CategoricalItem struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

type MultistatData struct {
	Items  []CategoricalItem  `json:"items"`
	Series []TimeseriesSeries `json:"series"`
}

type TableData struct {
	Rows []map[string]any `json:"rows"`
}

func mapPanelRows(renderType string, rows []Row) (any, error) {
	switch renderType {
	case renderStat:
		return mapStat(rows)
	case renderTimeseries:
		return mapTimeseries(rows)
	case renderCategorical:
		return mapCategorical(rows)
	case renderMultistat:
		return mapMultistat(rows)
	case renderTable:
		return mapTable(rows)
	default:
		return nil, errInternal(fmt.Sprintf("unsupported render type %q", renderType), nil)
	}
}

func mapStat(rows []Row) (StatData, error) {
	if len(rows) == 0 {
		return StatData{}, nil
	}
	row := rows[0]
	current, currentOK, err := optionalFloatColumn(row, "current")
	if err != nil {
		return StatData{}, err
	}
	previous, previousOK, err := optionalFloatColumn(row, "previous")
	if err != nil {
		return StatData{}, err
	}
	change, changeOK, err := optionalFloatColumn(row, "change_percent")
	if err != nil {
		return StatData{}, err
	}
	data := StatData{}
	if currentOK {
		data.Current = &current
	}
	if previousOK {
		data.Previous = &previous
	}
	if changeOK {
		data.ChangePercent = &change
	}
	return data, nil
}

func mapTimeseries(rows []Row) (TimeseriesData, error) {
	series := map[string][]TimeseriesPoint{}
	for _, row := range rows {
		ts, err := timeColumn(row, "timestamp")
		if err != nil {
			return TimeseriesData{}, err
		}
		name, err := stringColumn(row, "series")
		if err != nil {
			return TimeseriesData{}, err
		}
		value, err := floatColumn(row, "value")
		if err != nil {
			return TimeseriesData{}, err
		}
		series[name] = append(series[name], TimeseriesPoint{Timestamp: ts, Value: value})
	}
	names := make([]string, 0, len(series))
	for name := range series {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]TimeseriesSeries, 0, len(names))
	for _, name := range names {
		points := series[name]
		sort.Slice(points, func(i, j int) bool { return points[i].Timestamp.Before(points[j].Timestamp) })
		out = append(out, TimeseriesSeries{Name: name, Points: points})
	}
	if out == nil {
		out = []TimeseriesSeries{}
	}
	return TimeseriesData{Series: out}, nil
}

func mapCategorical(rows []Row) (CategoricalData, error) {
	items := make([]CategoricalItem, 0, len(rows))
	for _, row := range rows {
		name, err := stringColumn(row, "name")
		if err != nil {
			return CategoricalData{}, err
		}
		value, err := floatColumn(row, "value")
		if err != nil {
			return CategoricalData{}, err
		}
		items = append(items, CategoricalItem{Name: name, Value: value})
	}
	return CategoricalData{Items: items}, nil
}

func mapMultistat(rows []Row) (MultistatData, error) {
	items := make([]CategoricalItem, 0)
	series := map[string][]TimeseriesPoint{}
	for _, row := range rows {
		ts, ok, err := optionalTimeColumn(row, "timestamp")
		if err != nil {
			return MultistatData{}, err
		}
		name, err := stringColumn(row, "name")
		if err != nil {
			return MultistatData{}, err
		}
		value, valueOK, err := optionalFloatColumn(row, "value")
		if err != nil {
			return MultistatData{}, err
		}
		if !valueOK {
			continue
		}
		if ok {
			series[name] = append(series[name], TimeseriesPoint{Timestamp: ts, Value: value})
			continue
		}
		items = append(items, CategoricalItem{Name: name, Value: value})
	}
	names := make([]string, 0, len(series))
	for name := range series {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]TimeseriesSeries, 0, len(names))
	for _, name := range names {
		points := series[name]
		sort.Slice(points, func(i, j int) bool { return points[i].Timestamp.Before(points[j].Timestamp) })
		out = append(out, TimeseriesSeries{Name: name, Points: points})
	}
	return MultistatData{Items: items, Series: out}, nil
}

func mapTable(rows []Row) (TableData, error) {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		copied := make(map[string]any, len(row))
		for key, value := range row {
			copied[key] = value
		}
		out = append(out, copied)
	}
	return TableData{Rows: out}, nil
}

func optionalTimeColumn(row Row, key string) (time.Time, bool, error) {
	raw, ok := row[key]
	if !ok || raw == nil {
		return time.Time{}, false, nil
	}
	if typed, isString := raw.(string); isString && strings.TrimSpace(typed) == "" {
		return time.Time{}, false, nil
	}
	parsed, err := timeColumn(row, key)
	if err != nil {
		return time.Time{}, false, err
	}
	return parsed, true, nil
}

func timeColumn(row Row, key string) (time.Time, error) {
	raw, ok := row[key]
	if !ok || raw == nil {
		return time.Time{}, errInternal(fmt.Sprintf("missing column %q", key), nil)
	}
	switch typed := raw.(type) {
	case time.Time:
		return typed.UTC(), nil
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return parsed.UTC(), nil
		}
		if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
			return parsed.UTC(), nil
		}
		if parsed, err := time.ParseInLocation("2006-01-02T15:04:05", typed, time.UTC); err == nil {
			return parsed.UTC(), nil
		}
		return time.Time{}, errInternal(fmt.Sprintf("column %q is not a timestamp", key), nil)
	default:
		return time.Time{}, errInternal(fmt.Sprintf("column %q is not a timestamp", key), nil)
	}
}

func stringColumn(row Row, key string) (string, error) {
	raw, ok := row[key]
	if !ok || raw == nil {
		return "", errInternal(fmt.Sprintf("missing column %q", key), nil)
	}
	switch typed := raw.(type) {
	case string:
		return typed, nil
	default:
		return fmt.Sprint(typed), nil
	}
}

func floatColumn(row Row, key string) (float64, error) {
	value, ok, err := optionalFloatColumn(row, key)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errInternal(fmt.Sprintf("missing column %q", key), nil)
	}
	return value, nil
}

func optionalFloatColumn(row Row, key string) (float64, bool, error) {
	raw, ok := row[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	var value float64
	switch typed := raw.(type) {
	case float64:
		value = typed
	case float32:
		value = float64(typed)
	case int:
		value = float64(typed)
	case int64:
		value = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false, errInternal(fmt.Sprintf("column %q is not a number", key), err)
		}
		value = parsed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false, nil
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false, errInternal(fmt.Sprintf("column %q is not a number", key), err)
		}
		value = parsed
	default:
		return 0, false, errInternal(fmt.Sprintf("column %q is not a number", key), nil)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, nil
	}
	return value, true, nil
}
