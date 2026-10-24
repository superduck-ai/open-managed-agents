package messages

import (
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func TestCompactionObservationKeepsUsageWithoutPublicAssistantEvents(t *testing.T) {
	for _, backend := range []string{"messages", "chat"} {
		for _, auxiliary := range []bool{true, false} {
			t.Run(backend+map[bool]string{true: " auxiliary", false: " ordinary"}[auxiliary], func(t *testing.T) {
				thinking := 0
				o := &responseObservation{auxiliary: auxiliary, streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_compaction"}, publishThinking: func(string, int) bool { thinking++; return true }}
				body := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"summary\",\"usage\":{\"input_tokens\":17}}}\n\n" +
					"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
					"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
					"data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"private summary\"}}\n\n" +
					"data: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
					"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
					"data: {\"type\":\"message_stop\"}\n\n"
				if backend == "chat" {
					o.chat = &chatObservation{}
					body = chatFrame(`{"id":"summary","choices":[{"index":0,"delta":{"reasoning_content":"private thought","content":"private summary"},"finish_reason":"stop"}],"usage":{"prompt_tokens":17,"completion_tokens":7}}`) + chatFrame(`[DONE]`)
				}
				_, _ = o.Write([]byte(body))
				o.finish()
				if o.result.ErrorType != "" || o.result.Usage.InputTokens == nil || *o.result.Usage.InputTokens != 17 || o.result.Usage.OutputTokens == nil || *o.result.Usage.OutputTokens != 7 {
					t.Fatalf("usage or completion lost: %+v", o.result)
				}
				if auxiliary {
					if thinking != 0 || len(o.result.Messages) != 0 || len(o.result.EventIDs) != 0 || len(o.result.ToolUses) != 0 || len(o.result.ToolUseIDs) != 0 {
						t.Fatalf("summary leaked public events: thinking=%d result=%+v", thinking, o.result)
					}
				} else if thinking != 1 || len(o.result.Messages) == 0 || len(o.result.EventIDs) == 0 {
					t.Fatalf("ordinary events suppressed: thinking=%d result=%+v", thinking, o.result)
				}
			})
		}
	}
}

func TestCompactionPurposeHeaderDoesNotReachUpstream(t *testing.T) {
	headers := http.Header{"X-Oma-Model-Purpose": []string{"compaction"}, "Anthropic-Version": []string{"2023-06-01"}}
	result := sanitizedRequestHeaders(headers)
	if result.Get("X-OMA-Model-Purpose") != "" || result.Get("Anthropic-Version") != "2023-06-01" {
		t.Fatalf("incorrect purpose/header forwarding: %v", result)
	}
}
