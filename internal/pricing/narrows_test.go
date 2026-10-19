package pricing

import (
	"encoding/json/v2"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"os"
	"testing"
)

func TestNarrowsInvalidCatalog(t *testing.T) {
	for _, raw := range []string{`{}`, `{"rules":[]}`, `{"rules":[{"model":"x","currency":"USD","billing_type":"llm"}]}`, `{"rules":[{"model":"x","currency":"CNY","billing_type":"llm","pricing":{"base":[{"key":"input_cost_per_token","per_token":"-1"}]}}]}`} {
		if _, err := ParseNarrows([]byte(raw)); err == nil {
			t.Fatal("接受无效目录", raw)
		}
	}
	for _, c := range []narrowsCondition{{}, {Greater: new(int64(1)), AtLeast: new(int64(2))}, {AtLeast: new(int64(0))}, {Greater: new(int64(10)), Maximum: new(int64(10))}} {
		if _, err := narrowsLowerBound(c); err == nil {
			t.Fatal("接受无效分档")
		}
	}
}

func TestNarrowsCatalogReconciliation(t *testing.T) {
	raw, err := os.ReadFile("testdata/narrows.json")
	if err != nil {
		t.Fatal(err)
	}
	prices, err := ParseNarrows(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 61 {
		t.Fatalf("模型数量 %d", len(prices))
	}
	models := map[string]billing.ModelPrice{}
	for _, p := range prices {
		models[p.Model] = p.Price
		if p.Price.ExchangeRate != "7.2" || p.Price.Source != "narrows" || p.Price.SourceCurrency != "CNY" {
			t.Fatal("价格快照缺少来源或汇率")
		}
	}
	calculator := billing.NewCalculator(models)
	raw, err = os.ReadFile("testdata/narrows-expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Model  string `json:"model"`
		Input  int64  `json:"input"`
		Output int64  `json:"output"`
		Cents  *int64 `json:"cents"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		got, ok := calculator.ModelRequestCents(tc.Model, billing.TokenUsage{InputTokens: tc.Input, OutputTokens: tc.Output})
		if (tc.Cents != nil) != ok || (ok && got != *tc.Cents) {
			t.Errorf("%s 输入 %d: %d/%v, 预期 %v", tc.Model, tc.Input, got, ok, tc.Cents)
		}
	}
}
