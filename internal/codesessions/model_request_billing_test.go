package codesessions

import (
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"testing"
)

func TestModelRequestBillingUsesProxyUsage(t *testing.T) {
	service := &Service{billing: billing.NewCalculator(map[string]billing.ModelPrice{"priced": {InputPerMTok: 3, OutputPerMTok: 15, CacheReadPerMTok: 0.3, CacheWritePerMTok: 3.75}})}
	usage := ModelRequestUsage{InputTokens: new(int64(1000000)), OutputTokens: new(int64(1000000)), CacheReadInputTokens: new(int64(1000000)), CacheCreationInputTokens: new(int64(1000000))}
	if got := service.modelRequestBilling("unknown", usage); got != nil {
		t.Fatalf("未定价模型产生账单：%v", got)
	}
	got := service.modelRequestBilling("priced", usage)
	if got["list_cost"] != "2205" || got["currency"] != "USD" {
		t.Fatalf("代理请求费用错误：%v", got)
	}
}
