package analytics

import (
	"encoding/json"
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Metrics struct {
	Requests           int64      `json:"requests"`
	InputTokens        int64      `json:"input_tokens"`
	OutputTokens       int64      `json:"output_tokens"`
	CacheReadTokens    int64      `json:"cache_read_input_tokens"`
	CacheWriteTokens   int64      `json:"cache_creation_input_tokens"`
	CacheWrite5mTokens int64      `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens int64      `json:"cache_write_1h_tokens"`
	SearchRequests     int64      `json:"web_search_requests"`
	ModelCents         int64      `json:"model_cost_cents,string"`
	SearchCents        int64      `json:"search_cost_cents,string"`
	ModelCost          string     `json:"model_cost"`
	SearchCost         string     `json:"search_cost"`
	ModelCosts         []ToolCost `json:"model_costs,omitempty"`
	ToolCosts          []ToolCost `json:"tool_costs,omitempty"`
	SubtotalCents      int64      `json:"subtotal_cents,string"`
	Unpriced           int64      `json:"unpriced_requests"`
	Incomplete         int64      `json:"incomplete_requests"`
}

type ToolCost struct {
	Name string `json:"name"`
	Cost string `json:"cost"`
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

func ApplyWebSearchCounts(report Report, counts []db.WebSearchDay) Report {
	byDay := map[string]map[string]*big.Rat{}
	dayCounts := map[string]int64{}
	totals := map[string]*big.Rat{}
	var totalSearches int64
	for _, count := range counts {
		name := count.Name
		if name == "" {
			name = "web_search"
		}
		dollars := billing.ResolveSearchPrice(count.CNY, count.Per, count.Exchange).Dollars(count.Searches)
		addDollars(totals, name, dollars)
		if byDay[count.Day] == nil {
			byDay[count.Day] = map[string]*big.Rat{}
		}
		addDollars(byDay[count.Day], name, dollars)
		dayCounts[count.Day] += count.Searches
		totalSearches += count.Searches
	}
	report.Summary = applySearchMoney(report.Summary, totalSearches, totals["web_search"])
	report.Summary.ToolCosts = namedCosts(totals)
	for i := range report.Daily {
		key := report.Daily[i].Key
		report.Daily[i].Metrics = applySearchMoney(report.Daily[i].Metrics, dayCounts[key], byDay[key]["web_search"])
		report.Daily[i].Metrics.ToolCosts = namedCosts(byDay[key])
	}
	return report
}

func namedCosts(amounts map[string]*big.Rat) []ToolCost {
	names := slices.Sorted(maps.Keys(amounts))
	costs := make([]ToolCost, 0, len(names))
	for _, name := range names {
		if amounts[name] == nil || amounts[name].Sign() <= 0 {
			continue
		}
		costs = append(costs, ToolCost{Name: name, Cost: billing.FormatDollars(amounts[name])})
	}
	if len(costs) == 0 {
		return nil
	}
	return costs
}

func addDollars(dst map[string]*big.Rat, name string, amount *big.Rat) {
	if amount == nil || amount.Sign() <= 0 {
		return
	}
	if dst[name] == nil {
		dst[name] = new(big.Rat)
	}
	dst[name].Add(dst[name], amount)
}

func storedModelDollars(row db.ModelUsage) (*big.Rat, bool) {
	if row.ModelCostUSD != "" {
		dollars := new(big.Rat)
		if _, ok := dollars.SetString(row.ModelCostUSD); !ok || dollars.Sign() < 0 {
			return nil, false
		}
		return dollars, true
	}
	var price billing.ModelPrice
	if json.Unmarshal(row.PriceSnapshot, &price) != nil {
		return nil, false
	}
	return price.BudgetDollars(billing.TokenUsage{
		InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
		CacheReadInputTokens: row.CacheReadTokens, CacheCreationInputTokens: row.CacheWriteTokens,
		CacheWrite5mTokens: row.CacheWrite5mTokens, CacheWrite1hTokens: row.CacheWrite1hTokens,
	})
}

func ApplyModelCosts(report Report, rows []db.ModelUsage) Report {
	added := map[string]*big.Rat{}
	byModel := map[string]map[string]*big.Rat{}
	modelTotals := map[string]*big.Rat{}
	removed := map[string]int64{}
	var removedTotal int64
	addedTotal := new(big.Rat)
	for _, row := range rows {
		dollars, ok := storedModelDollars(row)
		if !ok {
			continue
		}
		name := row.Model
		if name == "" {
			name = "unknown"
		}
		if added[row.Day] == nil {
			added[row.Day] = new(big.Rat)
			byModel[row.Day] = map[string]*big.Rat{}
		}
		added[row.Day].Add(added[row.Day], dollars)
		addDollars(byModel[row.Day], name, dollars)
		addDollars(modelTotals, name, dollars)
		addedTotal.Add(addedTotal, dollars)
		removed[row.Day] += row.ModelCents
		removedTotal += row.ModelCents
	}
	report.Summary.ModelCost = billing.FormatDollars(mixCents(report.Summary.ModelCents-removedTotal, addedTotal))
	report.Summary.ModelCosts = namedCosts(modelTotals)
	for i := range report.Daily {
		day := report.Daily[i].Key
		report.Daily[i].Metrics.ModelCost = billing.FormatDollars(mixCents(report.Daily[i].Metrics.ModelCents-removed[day], added[day]))
		report.Daily[i].Metrics.ModelCosts = namedCosts(byModel[day])
	}
	return report
}

func mixCents(cents int64, extra *big.Rat) *big.Rat {
	total := new(big.Rat).SetFrac64(cents, 100)
	if extra != nil {
		total.Add(total, extra)
	}
	return total
}

func applySearchMoney(metrics Metrics, searches int64, dollars *big.Rat) Metrics {
	metrics.SearchRequests = searches
	metrics.SearchCents = billing.DollarsToCents(dollars)
	metrics.SearchCost = billing.FormatDollars(dollars)
	metrics.SubtotalCents = metrics.ModelCents + metrics.SearchCents
	return metrics
}

func add(m Metrics, row db.UsageAggregate) Metrics {
	m.Requests += row.Requests
	m.InputTokens += row.InputTokens
	m.OutputTokens += row.OutputTokens
	m.CacheReadTokens += row.CacheReadTokens
	m.CacheWriteTokens += row.CacheWriteTokens
	m.CacheWrite5mTokens += row.CacheWrite5mTokens
	m.CacheWrite1hTokens += row.CacheWrite1hTokens
	m.ModelCents += row.ModelCents
	m.SubtotalCents = m.ModelCents
	m.Unpriced += row.Unpriced
	m.Incomplete += row.Incomplete
	return m
}
