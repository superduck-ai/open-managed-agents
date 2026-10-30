package metering

import (
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestProviderCostOverridesEstimate(t *testing.T) {
	for _, tc := range []struct {
		name, body, content string
		expected            int64
		actual              bool
	}{
		{"普通", `{"type":"message","usage":{"input_tokens":1000},"gateway":{"amount":"0.25"}}`, "application/json", 25, true},
		{"零费用", `{"type":"message","usage":{"input_tokens":1000},"gateway":{"amount":"0"}}`, "application/json", 0, true},
		{"缺失回退", `{"type":"message","usage":{"input_tokens":1000}}`, "application/json", 1, false},
		{"非法回退", `{"type":"message","usage":{"input_tokens":1000},"gateway":{"amount":"-1"}}`, "application/json", 1, false},
		{"流式", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1000}}}\n\ndata: {\"type\":\"message_stop\",\"gateway\":{\"amount\":\"0.25\"}}\n\n", "text/event-stream", 25, true},
		{"截断", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1000}}}\n\n", "text/event-stream", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordStore{}
			calculator := billing.NewCalculator(map[string]billing.ModelPrice{"test/model": {Rates: &billing.Rates{Input: new("10")}}})
			calculator.ResponseCosts = map[string]billing.ResponseCostRule{"test": {AmountPointer: "/gateway/amount", StreamEvent: "message_stop", Currency: "USD", ExchangeRate: "1"}}
			transport := &Transport{Store: store, Calculator: calculator, Logger: slog.New(slog.DiscardHandler), Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.content}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			r := meteredTestRequest(t)
			r = r.WithContext(WithModel(r.Context(), "test/model"))
			response, err := transport.RoundTrip(r)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			last := store.records[len(store.records)-1]
			if len(store.records) != 2 || last.ModelCents == nil || *last.ModelCents != tc.expected || (len(last.ProviderCost) > 0) != tc.actual {
				t.Fatalf("结算错误或重复计费：%+v", last)
			}
		})
	}
}
