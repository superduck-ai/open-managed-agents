package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"
)

const dashboardQueryConcurrency = 6

type Row map[string]any

type DashboardRequest struct {
	Tab   string
	Model string
	Tool  string
}

var dashboardIntervals = []time.Duration{
	time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	time.Hour,
	3 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
}

const dashboardMaxPoints = 100

const dashboardLookback = 24 * time.Hour

func dashboardInterval(window Window) int64 {
	span := window.End.Sub(window.Start)
	for _, interval := range dashboardIntervals {
		if span <= interval*dashboardMaxPoints {
			return interval.Milliseconds()
		}
	}
	return dashboardIntervals[len(dashboardIntervals)-1].Milliseconds()
}

func previousWindow(window Window) Window {
	return Window{Start: window.Start.Add(-window.End.Sub(window.Start)), End: window.End}
}

func (b *OpenObserve) Dashboard(ctx context.Context, scope Scope, window Window, queries []string, request DashboardRequest) (map[string][]Row, error) {
	catalog, err := dashboards()
	if err != nil {
		return nil, invalidDashboard(err)
	}
	source, err := b.scopedQuery(scope)
	if err != nil {
		return nil, err
	}
	params := sqlParams{Source: source, Start: window.Start.UnixMilli(), End: window.End.UnixMilli(), Interval: dashboardInterval(window), Model: request.Model, Tool: request.Tool}
	results := make([][]Row, len(queries))
	errs := make([]error, len(queries))
	slots := make(chan struct{}, dashboardQueryConcurrency)
	var group sync.WaitGroup
	for index, name := range queries {
		group.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			results[index], errs[index] = b.dashboardQuery(ctx, catalog, name, window, params)
		})
	}
	group.Wait()
	rows := make(map[string][]Row, len(queries))
	for index, name := range queries {
		if errs[index] != nil {
			return nil, errs[index]
		}
		rows[name] = results[index]
	}
	return rows, nil
}

func (b *OpenObserve) dashboardQuery(ctx context.Context, catalog *dashboardCatalog, name string, window Window, params sqlParams) ([]Row, error) {
	spec := catalog.Queries[name]
	if spec.Window == windowCompare {
		window = previousWindow(window)
	}
	params.From = window.Start.UnixMilli()
	if spec.Lookback {
		window.Start = window.Start.Add(-dashboardLookback)
	}
	sql, err := catalog.render(name, params)
	if err != nil {
		return nil, invalidDashboard(err)
	}
	body, err := b.searchRaw(ctx, window, sql, spec.Size)
	if err != nil {
		return nil, err
	}
	return decodeRows(body)
}

func decodeRows(body []byte) ([]Row, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var result struct {
		Hits []Row `json:"hits"`
	}
	if err := decoder.Decode(&result); err != nil {
		return nil, invalidResponse(err)
	}
	return result.Hits, nil
}
