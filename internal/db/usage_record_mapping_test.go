package db

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

func TestModelRequestDollarsKeepsSixPlaces(t *testing.T) {
	raw := []byte(`{"_model_price":{"exchange_rate":"7.2","rates":{"input":"0.14","output":"0.42","cache_read":"0.0042"}},"model_usage":{"input_tokens":369,"output_tokens":127,"cache_read_input_tokens":3968}}`)
	if got := billing.FormatDollars(modelRequestDollars(raw)); got != "0.000017" {
		t.Fatalf("snapshot dollars = %s", got)
	}
	legacy := []byte(`{"billing":{"list_cost":"2","currency":"USD"}}`)
	if got := billing.FormatDollars(modelRequestDollars(legacy)); got != "0.020000" {
		t.Fatalf("legacy cents = %s", got)
	}
}

func TestSessionUsageDollarsAddsRecordedToolPrice(t *testing.T) {
	_, tool := sessionUsageParts(nil, []toolPriceCountRow{{CNY: "5", Per: "1000", Exchange: "7.2", Searches: 2}})
	if got := billing.FormatDollars(tool); got != "0.001389" {
		t.Fatalf("tool dollars = %s", got)
	}
}

func TestSessionUsageRecordUnknownAndCache(t *testing.T) {
	for _, payload := range []string{`{}`, `{"billing":{"list_cost":"-2","currency":"USD"}}`, `{"billing":{"list_cost":"2","currency":"EUR"}}`} {
		record, err := sessionUsageRecord(SessionEvent{Payload: json.RawMessage(payload)})
		if err != nil || record.ModelCents != nil || record.Complete {
			t.Fatalf("未知计量被转换为已确认费用：%+v", record)
		}
	}
	payload := `{"model":"priced-model","model_usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":30,"cache_creation_input_tokens":40,"cache_creation":{"ephemeral_5m_input_tokens":15,"ephemeral_1h_input_tokens":25},"server_tool_use":{"web_search_requests":3}},"billing":{"list_cost":"7","currency":"USD"}}`
	record, err := sessionUsageRecord(SessionEvent{ExternalID: "event", Payload: json.RawMessage(payload)})
	if err != nil || !record.Complete || record.ModelCents == nil || *record.ModelCents != 7 || record.SearchRequests != 0 || record.SearchCents != 0 || record.CacheWriteTokens != 40 || record.CacheWrite5mTokens != 15 || record.CacheWrite1hTokens != 25 {
		t.Fatalf("缓存或费用映射错误：%+v", record)
	}
}

func TestSessionUsageRecordRejectsMalformedPayload(t *testing.T) {
	for _, payload := range []string{`{`, `{"model_usage":{"input_tokens":"invalid"}}`, `{"_model_price":{"rates":4}}`} {
		if _, err := sessionUsageRecord(SessionEvent{Payload: json.RawMessage(payload)}); err == nil {
			t.Fatalf("invalid usage payload was accepted: %s", payload)
		}
	}
}

func TestSessionUsageRecordStoresBudgetAmountWithoutInputTokens(t *testing.T) {
	price := billing.ModelPrice{Rates: &billing.Rates{Input: new("3"), Output: new("15")}}
	payload, err := jsonv2.Marshal(sessionUsageEvent{
		Model: "model", PriceSnapshot: &price,
		Usage: &billing.Usage{CacheReadTokens: new(int64(1_000_000))},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := sessionUsageRecord(SessionEvent{Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if record.ModelCostUSD != "3.000000" {
		t.Fatalf("model_cost_usd = %s", record.ModelCostUSD)
	}
}

func TestSessionUsageRecordUsesStrictSnapshotPrice(t *testing.T) {
	for _, tc := range []struct {
		name string
		rate *string
		want *int64
	}{
		{name: "missing cache rate"},
		{name: "free cache", rate: new("0"), want: new(int64(0))},
		{name: "known cache rate", rate: new("0.3"), want: new(int64(30))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			price := billing.ModelPrice{Rates: &billing.Rates{Input: new("3"), Output: new("15"), CacheRead: tc.rate}}
			payload, err := jsonv2.Marshal(sessionUsageEvent{
				Model: "model", PriceSnapshot: &price,
				Usage:   &billing.Usage{InputTokens: new(int64(0)), OutputTokens: new(int64(0)), CacheReadTokens: new(int64(1000000))},
				Billing: &sessionUsageBilling{ListCost: "300", Currency: "USD"},
			})
			if err != nil {
				t.Fatal(err)
			}
			record, err := sessionUsageRecord(SessionEvent{Payload: payload})
			if err != nil {
				t.Fatal(err)
			}
			if !record.Complete || (record.ModelCents == nil) != (tc.want == nil) {
				t.Fatalf("unexpected completeness or price: %+v", record)
			}
			if tc.want != nil && *record.ModelCents != *tc.want {
				t.Fatalf("ledger charge = %d, want %d", *record.ModelCents, *tc.want)
			}
			if len(record.PriceSnapshot) == 0 {
				t.Fatal("price snapshot was lost")
			}
		})
	}
}
