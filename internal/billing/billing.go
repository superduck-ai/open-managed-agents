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
	"time"
)

const (
	CurrencyUSD = "USD"

	// CMA list prices for server tools and runtime, expressed in cents.
	WebSearchCentsPerRequest = 1 // $10 per 1,000 searches
	ActiveTimeCentsPerHour   = 8 // $0.08 per hour

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

// ModelPrice 保存每百万 Token 的原币价格和美元换算快照。
type ModelPrice struct {
	SourceCurrency string          `json:"source_currency,omitempty" yaml:"-"`
	ExchangeRate   string          `json:"exchange_rate,omitempty" yaml:"-"`
	Source         string          `json:"source,omitempty" yaml:"-"`
	SourceModel    string          `json:"source_model,omitempty" yaml:"-"`
	PricedAt       time.Time       `json:"priced_at,omitzero" yaml:"-"`
	Overrides      []PriceOverride `json:"overrides,omitempty" yaml:"-"`
	Rates          *Rates          `json:"rates,omitempty" yaml:"-"`
	Version        string          `json:"version,omitempty" yaml:"-"`
	ProviderUUID   string          `json:"provider_uuid,omitempty" yaml:"-"`
}

// Calculator prices model requests from configured list prices.
type Calculator struct {
	loader PriceLoader
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
	price, ok := c.Models[normalizeModelName(model)]
	return ok && (price.Rates != nil && price.Rates.Input != nil && price.Rates.Output != nil)
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

type TokenUsage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	CacheWrite5mTokens       int64
	CacheWrite1hTokens       int64
}

func (c *Calculator) ModelRequestCents(model string, usage TokenUsage) (cents int64, ok bool) {
	if c == nil {
		return 0, false
	}
	price, priced := c.Models[normalizeModelName(model)]
	if !priced {
		return 0, false
	}
	if price.Rates == nil {
		return 0, false
	}
	return price.effectiveRates(usage).cost(usage, price.ExchangeRate)
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

func (c *Calculator) UnpricedSnapshotModels(snapshot json.RawMessage) []string {
	var agent struct {
		Model json.RawMessage `json:"model"`
	}
	if len(snapshot) == 0 || c == nil || json.Unmarshal(snapshot, &agent) != nil {
		return nil
	}
	var modelID string
	if json.Unmarshal(agent.Model, &modelID) != nil {
		var model struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(agent.Model, &model) != nil {
			return nil
		}
		modelID = model.ID
	}
	if modelID == "" {
		return nil
	}
	return c.UnpricedModels([]string{modelID})
}

func normalizeModelName(model string) string {
	return strings.TrimSpace(model)
}
