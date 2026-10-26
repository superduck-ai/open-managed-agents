package pricing

import (
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"testing"
	"time"
)

func TestParsePrices(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":[{"id":"bad","pricing":{"prompt":"-2"}}]}`, `{"data":[{"id":"bad","pricing":{"overrides":[{"utc_start":300}]}}]}`} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Fatal("非法目录未拒绝")
		}
	}
	prices, err := Parse([]byte(`{"data":[{"id":"anthropic/test","pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0","input_cache_write":"0.00000375","input_cache_write_1h":"0.000006","overrides":[{"min_prompt_tokens":100000,"prompt":"0.000006"},{"utc_start":2200,"utc_end":200,"completion":"0.000030"}]}}]}`))
	if err != nil || len(prices) != 1 {
		t.Fatal(err)
	}
	p := prices[0].Price
	if *p.Rates.CacheWrite5m != "3.750000000000" || *p.Rates.CacheWrite1h != "6.000000000000" || p.Rates.CacheWrite != nil || *p.Rates.CacheRead != "0.000000000000" {
		t.Fatalf("缓存映射错误: %+v", p.Rates)
	}
	p.PricedAt = time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	calc := billing.NewCalculator(map[string]billing.ModelPrice{"test": p})
	cents, ok := calc.ModelRequestCents("test", billing.TokenUsage{InputTokens: 1000000, OutputTokens: 1000000})
	if !ok || cents != 3600 {
		t.Fatalf("分档和跨日时间窗计费=%d", cents)
	}
}

func TestUnknownRouterPrice(t *testing.T) {
	prices, err := Parse([]byte(`{"data":[{"id":"openrouter/auto","pricing":{"prompt":"-1","completion":"-1"}}]}`))
	if err != nil || prices[0].Price.Rates.Input != nil || prices[0].Price.Rates.Output != nil {
		t.Fatal("动态路由未知价被当成免费", err)
	}
}
