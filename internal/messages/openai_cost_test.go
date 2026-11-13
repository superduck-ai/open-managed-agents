package messages

import (
	"fmt"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestChatSessionActualCostBothModes(t *testing.T) {
	for _, stream := range []bool{false, true} {
		o := &responseObservation{chat: &chatObservation{}, streaming: stream, request: &codesessions.ModelRequest{ResponseCost: &billing.ResponseCostRule{AmountPointer: "/gateway/amount", Currency: "CNY", ExchangeRate: "7.2"}}}
		usage := `"usage":{"prompt_tokens":1000,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":200}},"gateway":{"amount":"7.2"}`
		body := `{"id":"chat_cost","choices":[{"index":0,"message":{"content":"你好"},"finish_reason":"stop"}],` + usage + `}`
		if stream {
			body = chatFrame(`{"id":"chat_cost","choices":[{"index":0,"delta":{"content":"你好"},"finish_reason":"stop"}]}`) + chatFrame(`{"id":"chat_cost","choices":[],`+usage+`}`) + chatFrame(`[DONE]`)
		}
		for _, b := range []byte(body) {
			_, _ = o.Write([]byte{b})
		}
		o.finish()
		if o.result.ErrorType != "" || o.result.ProviderCost == nil || o.result.ProviderCost.USDCents != 100 || len(o.result.Messages) != 1 || *o.result.Usage.InputTokens != 800 || *o.result.Usage.CacheReadInputTokens != 200 {
			t.Fatalf("%v: %+v", stream, o.result)
		}
	}
}

func TestResponsesSessionTerminalReceipt(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, status := range []string{"completed", "incomplete", "failed"} {
			t.Run(fmt.Sprintf("%v/%s", stream, status), func(t *testing.T) {
				o := &responseObservation{responses: &responsesObservation{}, streaming: stream, request: &codesessions.ModelRequest{ResponseCost: &billing.ResponseCostRule{AmountPointer: "/gateway/amount", Currency: "CNY", ExchangeRate: "7.2"}}}
				body := `{"id":"resp_cost","status":"` + status + `","output":[{"id":"msg_cost","type":"message","content":[{"type":"output_text","text":"你好"}]}],"usage":{"input_tokens":1000,"output_tokens":20,"input_tokens_details":{"cached_tokens":200}},"gateway":{"amount":"7.2"}}`
				if stream {
					body = chatFrame(`{"type":"response.` + status + `","response":` + body + `}`)
				}
				for _, b := range []byte(body) {
					_, _ = o.Write([]byte{b})
				}
				o.finish()
				if o.result.ProviderCost == nil || o.result.ProviderCost.USDCents != 100 || *o.result.Usage.InputTokens != 800 || *o.result.Usage.CacheReadInputTokens != 200 || (o.result.ErrorType == "") != (status == "completed") {
					t.Fatalf("%+v", o.result)
				}
				if status == "completed" && (len(o.result.Messages) != 1 || *o.result.Messages[0].Content[0].Text != "你好") {
					t.Fatal("丢失响应正文")
				}
			})
		}
	}
}

func TestResponsesEOFFinalizerDoesNotReenter(t *testing.T) {
	o := &responseObservation{responses: &responsesObservation{}, request: &codesessions.ModelRequest{}}
	o.buffer = []byte(`{"id":"resp_eof","status":"completed","output":[]}`)
	calls := 0
	o.onComplete = func() { calls++ }
	o.finish()
	if calls != 0 || !o.complete {
		t.Fatal("非流式结束回调重入")
	}
}

func TestResponsesContentIdentityMatchesSDK(t *testing.T) {
	o := &responseObservation{responses: &responsesObservation{}, request: &codesessions.ModelRequest{CodeSessionID: "cse_responses"}}
	_, _ = o.Write([]byte(`{"id":"resp_identity","status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"private one"},{"type":"summary_text","text":"private two"}]},{"type":"message","content":[{"type":"output_text","text":"hello"},{"type":"output_text","text":" world"}]}]}`))
	o.finish()
	if o.result.ErrorType != "" || len(o.result.Messages) != 3 {
		t.Fatalf("内容块与 SDK 不一致：%+v", o.result)
	}
	for i, message := range o.result.Messages {
		kind := "agent.thinking"
		if i == 2 {
			kind = "agent.message"
		}
		if message.ID != maevents.StableAssistantEventID("cse_responses", "resp_identity", i, kind) {
			t.Fatalf("内容块身份不一致：%+v", message)
		}
		if i < 2 && len(message.Content) != 0 {
			t.Fatal("思考正文不应公开")
		}
	}
	if *o.result.Messages[2].Content[0].Text != "hello world" {
		t.Fatal("同一 message 文本未合并")
	}
}
