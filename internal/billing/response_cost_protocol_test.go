package billing

import "testing"

func TestResponseCostProtocolConfiguration(t *testing.T) {
	rule := &ResponseCostRule{Currency: "CNY", ExchangeRate: "7.2", Protocols: map[string]ResponseCostProtocol{
		"chat_completions": {NonStreaming: &ResponseCostLocation{AmountPointer: "/receipt/charged"}, Streaming: &ResponseCostLocation{AmountPointer: "/receipt/charged", EventPointer: "/kind", Events: []string{"settled"}}},
		"responses":        {Streaming: &ResponseCostLocation{AmountPointer: "/bill/total", EventPointer: "/kind", Events: []string{"paid"}}},
	}}
	if err := rule.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		protocol string
		stream   bool
		body     string
		want     bool
	}{
		{"chat_completions", true, `{"kind":"pending","receipt":{"charged":"7.2"}}`, false},
		{"messages", false, `{"receipt":{"charged":"7.2"}}`, false},
		{"responses", false, `{"bill":{"total":"7.2"}}`, false},
		{"chat_completions", false, `{"receipt":{"charged":"7.2"}}`, true},
		{"chat_completions", true, `{"kind":"settled","receipt":{"charged":"7.2"}}`, true},
		{"responses", true, `{"kind":"paid","bill":{"total":"7.2"}}`, true},
	} {
		cost := rule.ExtractFor(tc.protocol, tc.stream, []byte(tc.body))
		if (cost != nil) != tc.want || cost != nil && cost.USDCents != 100 {
			t.Fatalf("%+v: %+v", tc, cost)
		}
	}
	legacy := &ResponseCostRule{AmountPointer: "/amount", Currency: "USD", ExchangeRate: "1"}
	if legacy.ExtractFor("responses", false, []byte(`{"amount":"1"}`)) != nil {
		t.Fatal("旧配置不能隐式启用其他协议")
	}
	rule.AmountPointer = "/amount"
	if rule.Validate() == nil {
		t.Fatal("混用新旧配置未拒绝")
	}
}
