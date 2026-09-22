// Package billing implements list-price metering for session budgets.
//
// 方案A: no ledger tables. Model request cost is computed when the platform
// synthesizes span.model_request_end and stored on the event payload as a
// CMA-compatible billing object {"list_cost": "53", "currency": "USD"}
// (integer cents as a string). Budget enforcement sums those payloads over
// the session's events and adds web-search and active-time metering.
package billing

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	CurrencyUSD = "USD"

	// CMA list prices for server tools and runtime, expressed in cents.
	WebSearchCentsPerRequest = 1 // $10 per 1,000 searches
	ActiveTimeCentsPerHour   = 8 // $0.08 per hour

	// TokensPerMillion is the price unit basis: model prices are quoted in
	// USD per million tokens.
	TokensPerMillion = 1_000_000
)

// Budget is a parsed session/deployment budget:
// {"type":"limit","max_list_cost":{"amount":"2500","currency":"USD"}}.
type Budget struct {
	Type        string     `json:"type"`
	MaxListCost CostAmount `json:"max_list_cost"`
}

// CostAmount is an integer-cent money value with an ISO-4217 currency code.
type CostAmount struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON renders the CMA wire shape with the amount as a string.
func (a CostAmount) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{
		"amount":   strconv.FormatInt(a.Amount, 10),
		"currency": a.Currency,
	})
}

// ParseBudget validates and parses a raw budget object. The amount must be an
// integer number of cents without decimals or leading zeros, greater than
// zero; USD is the only supported currency.
func ParseBudget(raw json.RawMessage) (Budget, error) {
	var wire struct {
		Type        string `json:"type"`
		MaxListCost *struct {
			Amount   *string `json:"amount"`
			Currency string  `json:"currency"`
		} `json:"max_list_cost"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Budget{}, fmt.Errorf("budget must be an object")
	}
	if wire.Type != "limit" {
		return Budget{}, fmt.Errorf("budget.type must be \"limit\"")
	}
	if wire.MaxListCost == nil || wire.MaxListCost.Amount == nil {
		return Budget{}, fmt.Errorf("budget.max_list_cost.amount is required")
	}
	amount, err := ParseCentsAmount(*wire.MaxListCost.Amount)
	if err != nil {
		return Budget{}, fmt.Errorf("budget.max_list_cost.%v", err)
	}
	currency := strings.TrimSpace(wire.MaxListCost.Currency)
	if currency == "" {
		currency = CurrencyUSD
	}
	if currency != CurrencyUSD {
		return Budget{}, fmt.Errorf("budget.max_list_cost.currency must be USD")
	}
	return Budget{Type: "limit", MaxListCost: CostAmount{Amount: amount, Currency: CurrencyUSD}}, nil
}

// ParseCentsAmount parses an integer-cent string amount: no sign, no decimals,
// no leading zeros, strictly positive.
func ParseCentsAmount(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("amount is required")
	}
	if value[0] == '-' || value[0] == '+' {
		return 0, fmt.Errorf("amount must be a positive integer number of cents")
	}
	if strings.ContainsAny(value, ".eE") {
		return 0, fmt.Errorf("amount must be an integer number of cents")
	}
	if len(value) > 1 && value[0] == '0' {
		return 0, fmt.Errorf("amount must not contain leading zeros")
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil || amount <= 0 {
		return 0, fmt.Errorf("amount must be a positive integer number of cents")
	}
	return amount, nil
}

// ModelPrice holds USD list prices per million tokens for one model.
type ModelPrice struct {
	InputPerMTok      float64 `json:"input" yaml:"input"`
	OutputPerMTok     float64 `json:"output" yaml:"output"`
	CacheReadPerMTok  float64 `json:"cache_read" yaml:"cache_read"`
	CacheWritePerMTok float64 `json:"cache_write" yaml:"cache_write"`
}

// Calculator prices model requests from configured list prices.
type Calculator struct {
	Models map[string]ModelPrice
}

// NewCalculator builds a calculator from configuration prices keyed by model
// name. A nil/empty config yields a calculator with no priced models.
func NewCalculator(prices map[string]ModelPrice) *Calculator {
	models := make(map[string]ModelPrice, len(prices))
	for name, price := range prices {
		models[strings.TrimSpace(name)] = price
	}
	return &Calculator{Models: models}
}

// HasModel reports whether the model has a configured list price.
func (c *Calculator) HasModel(model string) bool {
	if c == nil {
		return false
	}
	_, ok := c.Models[normalizeModelName(model)]
	return ok
}

// UnpricedModels returns the sorted subset of models without a list price.
func (c *Calculator) UnpricedModels(models []string) []string {
	if c == nil {
		return append([]string{}, models...)
	}
	missing := make([]string, 0)
	for _, model := range models {
		if !c.HasModel(model) {
			missing = append(missing, model)
		}
	}
	sort.Strings(missing)
	return missing
}

// ModelRequestCents prices one model request from its usage counters. Token
// usage may use either camelCase (Claude Code modelUsage) or snake_case keys.
// The result is rounded to the nearest cent. ok is false when the model has no
// list price.
func (c *Calculator) ModelRequestCents(model string, usage any) (cents int64, ok bool) {
	if c == nil {
		return 0, false
	}
	price, priced := c.Models[normalizeModelName(model)]
	if !priced {
		return 0, false
	}
	counters := usageCounters(usage)
	// Anthropic usage reports non-cache input separately: input_tokens excludes
	// cache_read_input_tokens and cache_creation_input_tokens, so all three are
	// billed at their own rates without subtraction.
	input := counters["inputTokens"] + counters["input_tokens"]
	output := counters["outputTokens"] + counters["output_tokens"]
	cacheRead := counters["cacheReadInputTokens"] + counters["cache_read_input_tokens"]
	cacheWrite := counters["cacheCreationInputTokens"] + counters["cache_creation_input_tokens"]
	dollars := (input*price.InputPerMTok +
		output*price.OutputPerMTok +
		cacheRead*price.CacheReadPerMTok +
		cacheWrite*price.CacheWritePerMTok) / TokensPerMillion
	return int64(math.Round(dollars * 100)), true
}

// usageCounters flattens token counters from the shapes seen in worker result
// payloads: either a per-model map {model: {counters}} or a flat counter map.
func usageCounters(usage any) map[string]float64 {
	counters := map[string]float64{}
	add := func(object map[string]any) {
		for key, value := range object {
			number, ok := value.(float64)
			if !ok {
				continue
			}
			counters[key] = number
		}
	}
	switch typed := usage.(type) {
	case map[string]any:
		if perModel, hasModelEntry := firstObjectEntry(typed); hasModelEntry {
			add(perModel)
			return counters
		}
		add(typed)
	case []any:
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				add(object)
			}
		}
	}
	return counters
}

func firstObjectEntry(object map[string]any) (map[string]any, bool) {
	for _, value := range object {
		if nested, ok := value.(map[string]any); ok {
			return nested, true
		}
	}
	return nil, false
}

// MeteringCents converts web-search request counts and active seconds into
// cents at the fixed CMA list prices.
func MeteringCents(webSearchRequests int64, activeSeconds float64) int64 {
	activeCents := int64(math.Round(activeSeconds / 3600 * ActiveTimeCentsPerHour))
	return webSearchRequests*WebSearchCentsPerRequest + activeCents
}

// TotalListCostCents is the authoritative list cost the budget is enforced
// against: model request costs stored on events plus runtime metering.
func TotalListCostCents(modelCents, webSearchRequests int64, activeSeconds float64) int64 {
	return modelCents + MeteringCents(webSearchRequests, activeSeconds)
}

// UnpricedSnapshotModels walks an agent snapshot and returns the sorted subset
// of model names referenced by "model" fields that have no list price. Used
// for create/update-time checks so a budget cannot be attached to workloads
// that would silently accrue no cost.
func (c *Calculator) UnpricedSnapshotModels(snapshot json.RawMessage) []string {
	if len(snapshot) == 0 || c == nil {
		return nil
	}
	var tree any
	if err := json.Unmarshal(snapshot, &tree); err != nil {
		return nil
	}
	seen := map[string]bool{}
	collectSnapshotModels(tree, seen)
	if len(seen) == 0 {
		return nil
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return c.UnpricedModels(models)
}

func collectSnapshotModels(node any, seen map[string]bool) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if key == "model" {
				if text, ok := value.(string); ok && text != "" {
					seen[text] = true
				}
			}
			collectSnapshotModels(value, seen)
		}
	case []any:
		for _, item := range typed {
			collectSnapshotModels(item, seen)
		}
	}
}

func normalizeModelName(model string) string {
	return strings.TrimSpace(model)
}
