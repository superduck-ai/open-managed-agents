package config

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

func TestLoadBillingModelPricesFromYAML(t *testing.T) {
	cfg, err := loadConfigTestYAML(t, `
billing:
  model_prices:
    claude-sonnet-4-5-20250929:
      input: 3.0
      output: 15.0
      cache_read: 0.3
      cache_write: 3.75
`)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	price, ok := cfg.Billing.ModelPrices["claude-sonnet-4-5-20250929"]
	if !ok {
		t.Fatalf("model price missing: %+v", cfg.Billing.ModelPrices)
	}
	if price != (billing.ModelPrice{InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheReadPerMTok: 0.3, CacheWritePerMTok: 3.75}) {
		t.Fatalf("unexpected parsed price: %+v", price)
	}
}
