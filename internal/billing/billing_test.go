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
		"claude-sonnet-4-5-20250929": {InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheReadPerMTok: 0.3, CacheWritePerMTok: 3.75},
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
	snake := map[string]any{"input_tokens": 1_000_000.0, "output_tokens": 1_000_000.0}
	if cents, ok := calculator.ModelRequestCents("claude-sonnet-4-5-20250929", snake); !ok || cents != 1800 {
		t.Fatalf("ModelRequestCents(snake) = (%d, %v), want (1800, true)", cents, ok)
	}

	// Cache tokens are excluded from the full input rate: input tokens already
	// include cache reads/writes in Claude usage payloads.
	withCache := map[string]any{
		"input_tokens":               1_000_000.0,
		"output_tokens":              1_000_000.0,
		"cache_read_input_tokens":    500_000.0,
		"cache_creation_input_tokens": 500_000.0,
	}
	// plain input = 0 after excluding cache; output 15.00 + cache read 0.15 +
	// cache write 1.875 = 17.025 USD → 1702 cents (float rounding).
	if cents, ok := calculator.ModelRequestCents("claude-sonnet-4-5-20250929", withCache); !ok || cents != 1702 {
		t.Fatalf("ModelRequestCents(cache) = (%d, %v), want (203, true)", cents, ok)
	}

	// camelCase keys from Claude Code modelUsage payloads.
	camel := map[string]any{"inputTokens": 1_000_000.0, "outputTokens": 1_000_000.0}
	if cents, ok := calculator.ModelRequestCents("claude-sonnet-4-5-20250929", camel); !ok || cents != 1800 {
		t.Fatalf("ModelRequestCents(camel) = (%d, %v), want (1800, true)", cents, ok)
	}

	// Per-model map shape: {model: {counters}}.
	perModel := map[string]any{"claude-sonnet-4-5-20250929": map[string]any{"input_tokens": 1_000_000.0, "output_tokens": 1_000_000.0}}
	if cents, ok := calculator.ModelRequestCents("claude-sonnet-4-5-20250929", perModel); !ok || cents != 1800 {
		t.Fatalf("ModelRequestCents(per-model) = (%d, %v), want (1800, true)", cents, ok)
	}

	if _, ok := calculator.ModelRequestCents("unlisted-model", snake); ok {
		t.Fatal("ModelRequestCents(unpriced) ok = true, want false")
	}
	var nilCalculator *Calculator
	if _, ok := nilCalculator.ModelRequestCents("claude-sonnet-4-5-20250929", snake); ok {
		t.Fatal("nil calculator ok = true, want false")
	}
}

func TestMeteringCents(t *testing.T) {
	if got := MeteringCents(10, 0); got != 10*WebSearchCentsPerRequest {
		t.Fatalf("MeteringCents(10 searches) = %d", got)
	}
	if got := MeteringCents(0, 3600); got != ActiveTimeCentsPerHour {
		t.Fatalf("MeteringCents(1h active) = %d", got)
	}
}

func TestTotalListCostCents(t *testing.T) {
	// 1800 active seconds = 0.5h at $0.08/h = 4 cents.
	got := TotalListCostCents(1800, 5, 1800)
	want := int64(1800 + 5*WebSearchCentsPerRequest + 4)
	if got != want {
		t.Fatalf("TotalListCostCents = %d, want %d", got, want)
	}
}
