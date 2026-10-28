package billing

import (
	"encoding/json"
	"testing"
)

func TestParseBudget(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Budget
		wantErr bool
	}{
		{
			name: "valid cents budget",
			raw:  `{"type":"limit","max_list_cost":{"amount":"2500","currency":"USD"}}`,
			want: Budget{Type: "limit", MaxListCost: CostAmount{Amount: 2500, Currency: CurrencyUSD}},
		},
		{
			name: "currency defaults to USD",
			raw:  `{"type":"limit","max_list_cost":{"amount":"1"}}`,
			want: Budget{Type: "limit", MaxListCost: CostAmount{Amount: 1, Currency: CurrencyUSD}},
		},
		{name: "missing type", raw: `{"max_list_cost":{"amount":"100"}}`, wantErr: true},
		{name: "wrong type", raw: `{"type":"monthly","max_list_cost":{"amount":"100"}}`, wantErr: true},
		{name: "missing max_list_cost", raw: `{"type":"limit"}`, wantErr: true},
		{name: "missing amount", raw: `{"type":"limit","max_list_cost":{"currency":"USD"}}`, wantErr: true},
		{name: "zero amount", raw: `{"type":"limit","max_list_cost":{"amount":"0"}}`, wantErr: true},
		{name: "negative amount", raw: `{"type":"limit","max_list_cost":{"amount":"-5"}}`, wantErr: true},
		{name: "decimal amount", raw: `{"type":"limit","max_list_cost":{"amount":"25.50"}}`, wantErr: true},
		{name: "leading zeros", raw: `{"type":"limit","max_list_cost":{"amount":"007"}}`, wantErr: true},
		{name: "non-USD currency", raw: `{"type":"limit","max_list_cost":{"amount":"100","currency":"EUR"}}`, wantErr: true},
		{name: "not an object", raw: `"2500"`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseBudget(json.RawMessage(test.raw))
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseBudget(%s) = %+v, want error", test.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBudget(%s) error = %v", test.raw, err)
			}
			if got != test.want {
				t.Fatalf("ParseBudget(%s) = %+v, want %+v", test.raw, got, test.want)
			}
		})
	}
}

func TestCostAmountMarshalJSON(t *testing.T) {
	data, err := json.Marshal(CostAmount{Amount: 53, Currency: CurrencyUSD})
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	var round map[string]string
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if round["amount"] != "53" || round["currency"] != "USD" {
		t.Fatalf("wire = %v, want amount=53 currency=USD", round)
	}
}

func TestBudgetMarshalKeepsTypeForStorage(t *testing.T) {
	parsed, err := ParseBudget(json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"1000","currency":"USD"}}`))
	if err != nil {
		t.Fatalf("ParseBudget error = %v", err)
	}
	data, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal error = %v", err)
	}
	if round["type"] != "limit" {
		t.Fatalf("marshaled budget = %v, want type=limit preserved for ParseBudget round-trip", round)
	}
}

func TestParseCentsAmount(t *testing.T) {
	valid := map[string]int64{"1": 1, "2500": 2500, " 42 ": 42}
	for raw, want := range valid {
		got, err := ParseCentsAmount(raw)
		if err != nil || got != want {
			t.Fatalf("ParseCentsAmount(%q) = (%d, %v), want (%d, nil)", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "0", "-5", "+5", "2.5", "1e3", "007", "abc"} {
		if _, err := ParseCentsAmount(raw); err == nil {
			t.Fatalf("ParseCentsAmount(%q) = nil error, want error", raw)
		}
	}
}

func TestCalculatorModelRequestCents(t *testing.T) {
	prices := map[string]ModelPrice{
		"claude-sonnet-4-5-20250929": {Rates: &Rates{Input: new("3.0"), Output: new("15.0"), CacheRead: new("0.3"), CacheWrite: new("3.75")}},
	}
	calculator := NewCalculator(prices)

	if !calculator.HasModel("claude-sonnet-4-5-20250929") {
		t.Fatal("HasModel(priced model) = false")
	}
	if calculator.HasModel("unlisted-model") {
		t.Fatal("HasModel(unpriced model) = true")
	}
	if missing := calculator.UnpricedModels([]string{"claude-sonnet-4-5-20250929", "b-model", "a-model"}); len(missing) != 2 || missing[0] != "a-model" || missing[1] != "b-model" {
		t.Fatalf("UnpricedModels = %v, want [a-model b-model]", missing)
	}

	// 1M input + 1M output tokens: 3.0 + 15.0 = 18.00 USD = 1800 cents.
	usage := TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	if cents, ok := calculator.ModelRequestCents("claude-sonnet-4-5-20250929", usage); !ok || cents != 1800 {
		t.Fatalf("ModelRequestCents(snake) = (%d, %v), want (1800, true)", cents, ok)
	}

	// Anthropic usage reports non-cache input separately: input_tokens excludes
	// cache reads/writes, so each counter is billed at its own rate.
	withCache := TokenUsage{
		InputTokens:              1_000_000.0,
		OutputTokens:             1_000_000.0,
		CacheReadInputTokens:     500_000.0,
		CacheCreationInputTokens: 500_000.0,
	}
	// input 3.00 + output 15.00 + cache read 0.15 + cache write 1.875 = 20.025

	if cents, ok := calculator.ModelRequestCents("claude-sonnet-4-5-20250929", withCache); !ok || cents != 2003 {
		t.Fatalf("ModelRequestCents(cache) = (%d, %v), want (2003, true)", cents, ok)
	}

	if _, ok := calculator.ModelRequestCents("unlisted-model", usage); ok {
		t.Fatal("ModelRequestCents(unpriced) ok = true, want false")
	}
	var nilCalculator *Calculator
	if _, ok := nilCalculator.ModelRequestCents("claude-sonnet-4-5-20250929", usage); ok {
		t.Fatal("nil calculator ok = true, want false")
	}
}

func TestBudgetRequestCentsMissingCacheRate(t *testing.T) {
	calculator := NewCalculator(map[string]ModelPrice{
		"priced": {Rates: &Rates{Input: new("3"), Output: new("15")}},
	})
	if !calculator.HasModel("priced") {
		t.Fatal("HasModel(priced) = false")
	}
	usage := TokenUsage{CacheReadInputTokens: 1_000_000}
	if _, ok := calculator.ModelRequestCents("priced", usage); ok {
		t.Fatal("ModelRequestCents priced a missing cache read rate")
	}
	cents, ok := calculator.BudgetRequestCents("priced", usage)
	if !ok || cents != 300 {
		t.Fatalf("BudgetRequestCents(cache read) = (%d, %v), want (300, true)", cents, ok)
	}

	anthropic := NewCalculator(map[string]ModelPrice{
		"priced": {Rates: &Rates{Input: new("3"), Output: new("15"), CacheRead: new("0.3"), CacheWrite5m: new("3.75"), CacheWrite1h: new("6")}},
	})
	unsplit := TokenUsage{CacheCreationInputTokens: 1_000_000}
	if _, ok := anthropic.ModelRequestCents("priced", unsplit); ok {
		t.Fatal("ModelRequestCents priced unsplit cache creation without CacheWrite")
	}
	cents, ok = anthropic.BudgetRequestCents("priced", unsplit)
	if !ok || cents != 375 {
		t.Fatalf("BudgetRequestCents(cache write) = (%d, %v), want (375, true)", cents, ok)
	}
}

func TestTokenDollarsKeepsSubCentAmounts(t *testing.T) {
	price := ModelPrice{ExchangeRate: "7.2", Rates: &Rates{Input: new("0.14"), Output: new("0.42"), CacheRead: new("0.0042")}}
	dollars, ok := price.TokenDollars(TokenUsage{InputTokens: 369, OutputTokens: 127, CacheReadInputTokens: 3968})
	if !ok || FormatDollars(dollars) != "0.000017" {
		t.Fatalf("sub-cent token cost = %s, ok %v", FormatDollars(dollars), ok)
	}
}

func TestToolCostCents(t *testing.T) {
	if got := ToolCostCents(Usage{}); got != 0 {
		t.Fatalf("no searches = %d", got)
	}
	one := ToolCostCents(Usage{ServerToolUse: &SearchUsage{Requests: new(int64(1))}})
	if one != 0 {
		t.Fatalf("one search = %d, want 0", one)
	}
	thousand := ToolCostCents(Usage{ServerToolUse: &SearchUsage{Requests: new(int64(1000))}})
	if thousand != 69 {
		t.Fatalf("1000 searches = %d, want 69 cents", thousand)
	}
	if got := FormatDollars(ToolDollars(Usage{})); got != "0.000000" {
		t.Fatalf("empty dollars = %s", got)
	}
	if got := FormatDollars(ToolDollars(Usage{ServerToolUse: &SearchUsage{Requests: new(int64(2))}})); got != "0.001389" {
		t.Fatalf("two searches = %s, want 0.001389", got)
	}
	if got := FormatDollars(ToolDollars(Usage{ServerToolUse: &SearchUsage{Requests: new(int64(1000))}})); got != "0.694444" {
		t.Fatalf("1000 searches = %s, want 0.694444", got)
	}
}

func TestRecordedSearchPriceDoesNotFollowCatalog(t *testing.T) {
	recorded := ResolveSearchPrice("5", "1000", "7.2")
	if FormatDollars(recorded.Dollars(1000)) != "0.694444" || FormatDollars(LegacySearchPrice().Dollars(2)) != "0.001389" {
		t.Fatalf("recorded price changed: 1000=%s 2=%s", FormatDollars(recorded.Dollars(1000)), FormatDollars(LegacySearchPrice().Dollars(2)))
	}
	if FormatDollars((SearchPrice{CNY: "10", Per: 1000, Exchange: "7.2"}).Dollars(1000)) != "1.388889" {
		t.Fatal("a later unit price must not be the recorded price")
	}
}

func TestModelRequestCentsDecimalRounding(t *testing.T) {
	calculator := NewCalculator(map[string]ModelPrice{"priced": {Rates: &Rates{Input: new("1"), Output: new("0")}}})
	for _, test := range []struct {
		tokens int64
		want   int64
	}{
		{4999, 0}, {5000, 1}, {5001, 1}, {20024999, 2002}, {20025000, 2003}, {20025001, 2003},
	} {
		got, ok := calculator.ModelRequestCents("priced", TokenUsage{InputTokens: test.tokens})
		if !ok || got != test.want {
			t.Fatalf("tokens=%v：got=%d，want=%d", test.tokens, got, test.want)
		}
	}
}

func TestSnapshotModelPricing(t *testing.T) {
	c := NewCalculator(map[string]ModelPrice{"priced": {Rates: &Rates{Input: new("1"), Output: new("0")}}})
	for _, test := range []struct {
		raw  string
		want string
	}{
		{`{"model":{"id":"unpriced"}}`, "unpriced"},
		{`{"model":"unpriced"}`, "unpriced"},
		{`{"model":{"id":"priced"},"metadata":{"model":"invoice-v2"},"tools":[{"model":"tool-data"}]}`, ""},
		{`{"model":"priced"}`, ""},
	} {
		got := c.UnpricedSnapshotModels(json.RawMessage(test.raw))
		if test.want == "" {
			if len(got) != 0 {
				t.Fatalf("错误识别模型：%v", got)
			}
			continue
		}
		if len(got) != 1 || got[0] != test.want {
			t.Fatalf("未定价模型：%v，预期 %s", got, test.want)
		}
	}
}
