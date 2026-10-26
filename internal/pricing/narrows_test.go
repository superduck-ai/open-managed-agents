package pricing

import (
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
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

func TestNarrowsTierReconciliation(t *testing.T) {
	for _, condition := range []string{`"input_tokens_gt":10000`, `"input_tokens_gte":10001`} {
		raw := fmt.Sprintf(`{"rules":[{"model":"test","currency":"CNY","billing_type":"llm","pricing":{"base":[{"key":"input_cost_per_token","per_token":"0.0000072"},{"key":"output_cost_per_token","per_token":"0.0000144"}],"tiers":[{"condition":{%s,"max_input_tokens":20000},"components":[{"key":"input_cost_per_token","per_token":"0.0000144"}]}]}}]}`, condition)
		prices, err := ParseNarrows([]byte(raw))
		if err != nil || len(prices) != 1 {
			t.Fatalf("目录解析失败：%v", err)
		}
		p := prices[0].Price
		if p.ExchangeRate != "7.2" || p.Source != "narrows" || p.SourceCurrency != "CNY" || p.SourceModel != "test" || p.Version == "" {
			t.Fatal("价格快照缺少来源或汇率")
		}
		calculator := billing.NewCalculator(map[string]billing.ModelPrice{"test": p})
		for _, tc := range []struct{ input, cents int64 }{{9999, 1}, {10000, 1}, {10001, 2}, {20000, 4}, {20001, 2}} {
			got, ok := calculator.ModelRequestCents("test", billing.TokenUsage{InputTokens: tc.input, OutputTokens: 1000})
			if !ok || got != tc.cents {
				t.Errorf("%s 输入 %d: %d/%v，预期 %d", condition, tc.input, got, ok, tc.cents)
			}
		}
	}
}

func TestNarrowsAmbiguousRates(t *testing.T) {
	for _, key := range []string{"thinking_output_cost_per_token", "output_cost_per_image_token", "implicit_cache_read_input_token_cost"} {
		raw := fmt.Sprintf(`{"rules":[{"model":"test","currency":"CNY","billing_type":"llm","pricing":{"base":[{"key":"input_cost_per_token","per_token":"0.0000072"},{"key":"output_cost_per_token","per_token":"0.0000144"},{"key":"cache_read_input_token_cost","per_token":"0.000001"},{"key":%q,"per_token":"0.00003"}]}}]}`, key)
		prices, err := ParseNarrows([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		calculator := billing.NewCalculator(map[string]billing.ModelPrice{"test": prices[0].Price})
		if _, ok := calculator.ModelRequestCents("test", billing.TokenUsage{InputTokens: 10000, OutputTokens: 1000, CacheReadInputTokens: 1000}); ok {
			t.Fatalf("无法区分 %s 时不应计费", key)
		}
	}
}
