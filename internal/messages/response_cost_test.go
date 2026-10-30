package messages

import (
	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"testing"
)

func TestSessionResponseCost(t *testing.T) {
	rule := &billing.ResponseCostRule{AmountPointer: "/gateway/amount", StreamEvent: "message_stop", Currency: "USD", ExchangeRate: "1"}
	for _, stream := range []bool{false, true} {
		body := `{"id":"msg_cost","type":"message","usage":{"input_tokens":1,"output_tokens":1},"gateway":{"amount":"0.125"}}`
		if stream {
			body = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_cost\"}}\n\ndata: {\"type\":\"message_stop\",\"gateway\":{\"amount\":\"0.125\"}}\n\n"
		}
		o := &responseObservation{streaming: stream, request: &codesessions.ModelRequest{ResponseCost: rule}}
		for _, b := range []byte(body) {
			_, _ = o.Write([]byte{b})
		}
		o.finish()
		if o.result.ProviderCost == nil || o.result.ProviderCost.USDCents != 13 {
			t.Fatalf("stream=%v: %+v", stream, o.result)
		}
	}
}
