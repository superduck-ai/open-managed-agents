package metering

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

func openAIResponseBody(protocol string, stream bool, amount string, status string) string {
	cost := `"gateway":{"amount":` + amount + `}`
	if protocol == "chat/completions" {
		usage := `"usage":{"prompt_tokens":1000,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":200}}`
		if !stream {
			return `{"id":"chat_test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],` + usage + `,` + cost + `}`
		}
		return "data: {\"id\":\"chat_test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你好\"},\"finish_reason\":\"stop\"}]}\r\n\r\ndata: {\"id\":\"chat_test\",\"choices\":[]," + usage + "," + cost + "}\r\n\r\ndata: [DONE]\r\n\r\n"
	}
	response := `{"id":"resp_test","object":"response","status":"` + status + `","output":[],"usage":{"input_tokens":1000,"output_tokens":20,"input_tokens_details":{"cached_tokens":200}},` + cost + `}`
	if !stream {
		return response
	}
	return "event: response." + status + "\ndata: {\"type\":\"response." + status + "\",\"response\":" + response + "}\n\n"
}

func TestOpenAIActualCostAccounting(t *testing.T) {
	for _, protocol := range []string{"chat/completions", "responses"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct {
				name, amount string
				cents        int64
				actual       bool
			}{
				{"费用未知", `null`, 1, false}, {"非法费用", `"-1"`, 1, false}, {"非字符串", `7.2`, 1, false}, {"实际费用", `"7.2"`, 100, true}, {"免费", `"0.000000000000"`, 0, true},
			} {
				t.Run(fmt.Sprintf("%s/%v/%s", protocol, stream, tc.name), func(t *testing.T) {
					store := &recordStore{}
					calculator := billing.NewCalculator(map[string]billing.ModelPrice{"test/model": {Rates: &billing.Rates{Input: new("10"), Output: new("0"), CacheRead: new("0")}}})
					calculator.ResponseCosts = map[string]billing.ResponseCostRule{"test": {AmountPointer: "/gateway/amount", Currency: "CNY", ExchangeRate: "7.2"}}
					body := openAIResponseBody(protocol, stream, tc.amount, "completed")
					content := "application/json"
					if stream {
						content = "text/event-stream"
					}
					transport := &Transport{Store: store, Calculator: calculator, Logger: slog.New(slog.DiscardHandler), Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {content}}, Body: io.NopCloser(strings.NewReader(body))}, nil
					})}
					request := meteredTestRequest(t)
					request.URL.Path = "/v1/" + protocol
					request = request.WithContext(WithModel(request.Context(), "test/model"))
					response, err := transport.RoundTrip(request)
					if err != nil {
						t.Fatal(err)
					}
					var output strings.Builder
					if _, err := io.Copy(&output, response.Body); err != nil {
						t.Fatal(err)
					}
					_ = response.Body.Close()
					_ = response.Body.Close()
					if output.String() != body || len(store.records) != 2 {
						t.Fatal("响应被改变或重复结算")
					}
					record := store.records[1]
					if record.ModelCents == nil || *record.ModelCents != tc.cents || (len(record.ProviderCost) > 0) != tc.actual || !record.Complete || record.InputTokens != 800 || record.CacheReadTokens != 200 || record.OutputTokens != 20 {
						t.Fatalf("结算错误：%+v", record)
					}
				})
			}
		}
	}
}

func TestResponsesTerminalCostAndInterruption(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, status := range []string{"completed", "incomplete", "failed"} {
			o := &observation{responses: true, streaming: stream, rule: &billing.ResponseCostRule{AmountPointer: "/gateway/amount", Currency: "CNY", ExchangeRate: "7.2"}}
			body := openAIResponseBody("responses", stream, `"7.2"`, status)
			for _, b := range []byte(body) {
				o.write([]byte{b})
			}
			o.eof = true
			o.end()
			if o.cost == nil || o.cost.USDCents != 100 || o.complete != (status == "completed") || o.failed != (status == "failed") {
				t.Fatalf("%v/%s: %+v", stream, status, o)
			}
		}
	}
	o := &observation{responses: true, streaming: true}
	o.write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))
	o.eof = true
	o.end()
	if o.complete || o.cost != nil {
		t.Fatal("断流被当成完整或免费")
	}
}
