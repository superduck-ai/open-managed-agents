package analytics

import (
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Metrics struct {
	Requests           int64 `json:"requests"`
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	CacheReadTokens    int64 `json:"cache_read_input_tokens"`
	CacheWriteTokens   int64 `json:"cache_creation_input_tokens"`
	CacheWrite5mTokens int64 `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens int64 `json:"cache_write_1h_tokens"`
	SearchRequests     int64 `json:"web_search_requests"`
	ModelCents         int64 `json:"model_cost_cents,string"`
	SearchCents        int64 `json:"search_cost_cents,string"`
	SubtotalCents      int64 `json:"subtotal_cents,string"`
	Unpriced           int64 `json:"unpriced_requests"`
	Incomplete         int64 `json:"incomplete_requests"`
}

type Bucket struct {
	Key     string  `json:"key"`
	Metrics Metrics `json:"metrics"`
}

type Report struct {
	Currency  string    `json:"currency"`
	CostBasis string    `json:"cost_basis"`
	Version   string    `json:"billing_version"`
	Included  []string  `json:"included_components"`
	Excluded  []string  `json:"excluded_components"`
	AsOf      time.Time `json:"as_of"`
	Start     time.Time `json:"starting_at"`
	End       time.Time `json:"ending_at"`
	Timezone  string    `json:"timezone"`
	Summary   Metrics   `json:"summary"`
	Daily     []Bucket  `json:"daily"`
}

func BuildReport(rows []db.UsageAggregate, query db.UsageQuery) Report {
	report := Report{Currency: "USD", CostBasis: "list_price_model_and_search", Version: "request_cents_v1",
		Included: []string{"model", "web_search"}, Excluded: []string{"code_execution", "session_runtime"},
		AsOf: query.AsOf, Start: query.Start, End: query.End, Timezone: query.Timezone, Daily: []Bucket{}}
	days := map[string]Metrics{}
	for _, row := range rows {
		report.Summary = add(report.Summary, row)
		days[row.Day] = add(days[row.Day], row)
	}
	location, _ := time.LoadLocation(query.Timezone)
	localStart := query.Start.In(location)
	for day := time.Date(localStart.Year(), localStart.Month(), localStart.Day(), 0, 0, 0, 0, location); day.Before(query.End); day = day.AddDate(0, 0, 1) {
		key := day.Format(time.DateOnly)
		report.Daily = append(report.Daily, Bucket{Key: key, Metrics: days[key]})
	}
	return report
}

func add(m Metrics, row db.UsageAggregate) Metrics {
	m.Requests += row.Requests
	m.InputTokens += row.InputTokens
	m.OutputTokens += row.OutputTokens
	m.CacheReadTokens += row.CacheReadTokens
	m.CacheWriteTokens += row.CacheWriteTokens
	m.CacheWrite5mTokens += row.CacheWrite5mTokens
	m.CacheWrite1hTokens += row.CacheWrite1hTokens
	m.SearchRequests += row.SearchRequests
	m.ModelCents += row.ModelCents
	m.SearchCents += row.SearchCents
	m.SubtotalCents = m.ModelCents + m.SearchCents
	m.Unpriced += row.Unpriced
	m.Incomplete += row.Incomplete
	return m
}
