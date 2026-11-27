package db

import (
	"encoding/json/v2"
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"testing"
)

func TestSessionUsagePreservesProviderCost(t *testing.T) {
	record, err := sessionUsageRecord(SessionEvent{Payload: []byte(`{"model":"test/model","model_usage":{"input_tokens":1000},"_model_price":{"rates":{"input":"10"}},"billing":{"list_cost":"100","currency":"USD","source":"provider"},"_provider_cost":{"amount":"7.2","currency":"CNY","exchange_rate":"7.2","usd_cents":100}}`)})
	if err != nil {
		t.Fatal(err)
	}
	var cost billing.ProviderCost
	if err := json.Unmarshal(record.ProviderCost, &cost); err != nil {
		t.Fatal(err)
	}
	if record.ModelCents == nil || *record.ModelCents != 100 || cost.Amount != "7.2" || cost.USDCents != 100 {
		t.Fatalf("实际费用未保留：%+v", record)
	}
}
