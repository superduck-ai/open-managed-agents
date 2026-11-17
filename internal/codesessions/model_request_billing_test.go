package codesessions

import (
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"testing"
)

func TestModelRequestBillingUsesProxyUsage(t *testing.T) {
	service := &Service{billing: billing.NewCalculator(map[string]billing.ModelPrice{"priced": {Rates: &billing.Rates{Input: new("3"), Output: new("15"), CacheRead: new("0.3"), CacheWrite: new("3.75")}}})}
	usage := ModelRequestUsage{InputTokens: new(int64(1000000)), OutputTokens: new(int64(1000000)), CacheReadInputTokens: new(int64(1000000)), CacheCreationInputTokens: new(int64(1000000))}
	if got := modelRequestBilling(service.billing, "unknown", usage); got != nil {
		t.Fatalf("未定价模型产生账单：%v", got)
	}
	got := modelRequestBilling(service.billing, "priced", usage)
	if got["list_cost"] != "2205" || got["currency"] != "USD" {
		t.Fatalf("代理请求费用错误：%v", got)
	}
}

func TestModelRequestBillingCountsMissingCacheRead(t *testing.T) {
	calculator := billing.NewCalculator(map[string]billing.ModelPrice{
		"priced": {Rates: &billing.Rates{Input: new("3"), Output: new("15")}},
	})
	if !calculator.HasModel("priced") {
		t.Fatal("HasModel(priced) = false")
	}
	got := modelRequestBilling(calculator, "priced", ModelRequestUsage{InputTokens: new(int64(0)), OutputTokens: new(int64(0)), CacheReadInputTokens: new(int64(1_000_000))})
	if got["list_cost"] != "300" || got["currency"] != "USD" {
		t.Fatalf("缺失缓存读取单价被记成 %v，期望 300 USD", got)
	}
}

func TestModelRequestBillingRequiresKnownUsage(t *testing.T) {
	calculator := billing.NewCalculator(map[string]billing.ModelPrice{"priced": {Rates: &billing.Rates{Input: new("3"), Output: new("15")}}})
	for _, usage := range []ModelRequestUsage{{}, {InputTokens: new(int64(0))}, {OutputTokens: new(int64(0))}} {
		if got := modelRequestBilling(calculator, "priced", usage); got != nil {
			t.Fatalf("未知用量被记成费用：%v", got)
		}
	}
	if got := modelRequestBilling(calculator, "priced", ModelRequestUsage{InputTokens: new(int64(0)), OutputTokens: new(int64(0))}); got["list_cost"] != "0" {
		t.Fatalf("明确零用量未计量：%v", got)
	}
}
