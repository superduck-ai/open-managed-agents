package codesessions

import (
	"encoding/json/v2"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"testing"
	"time"
)

func TestProviderCostFeedsSessionBudget(t *testing.T) {
	service := newTestService(t, nil)
	sink := &brainModelEventSink{}
	service.SetPublicEventSink(sink)
	request := &ModelRequest{StartID: "cost-start", ThreadID: "thread", Model: "test/model", calculator: billing.NewCalculator(map[string]billing.ModelPrice{"test/model": {Rates: &billing.Rates{Input: new("10")}}})}
	result := ModelRequestResult{EndedAt: time.Now(), Usage: ModelRequestUsage{InputTokens: new(int64(1000))}, ProviderCost: &billing.ProviderCost{Amount: "7.2", Currency: "CNY", ExchangeRate: "7.2", USDCents: 100}}
	if err := service.EndModelRequest(t.Context(), request, result); err != nil {
		t.Fatal(err)
	}
	var event modelRequestEvent
	if len(sink.events) != 1 {
		t.Fatalf("事件数错误：%d", len(sink.events))
	}
	if err := json.Unmarshal(sink.events[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.Billing["list_cost"] != "100" || event.Billing["source"] != "provider" || event.ProviderCost == nil || event.ProviderCost.Amount != "7.2" {
		t.Fatalf("预算未采用实际费用：%+v", event)
	}
}
