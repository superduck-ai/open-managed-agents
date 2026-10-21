package messages

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestChatRequestRejectsUnsupportedModes(t *testing.T) {
	for _, body := range []string{`{}`, `{"stream":false}`, `{"stream":true,"n":2}`, `{"stream":true,"n":0}`, `{"stream":"true"}`, `{"stream":true,"n":"1"}`} {
		if err := validateChatRequest([]byte(body)); !errors.Is(err, errChatStreamRequired) {
			t.Fatalf("body=%s err=%v", body, err)
		}
	}
	handler := &Handler{}
	for _, principal := range []auth.Principal{{}, {WorkspaceUUID: "workspace"}} {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		request = request.WithContext(auth.WithPrincipal(request.Context(), principal))
		response := httptest.NewRecorder()
		handler.ChatCompletions(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("status=%d", response.Code)
		}
	}
}

func TestChatObservationRejectsInvalidStreams(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"truncated", chatFrame(`{"id":"chat_1","choices":[{"index":0,"delta":{"content":"partial"}}]}`), "incomplete_response"},
		{"no finish", chatFrame(`{"id":"chat_1","choices":[{"index":0,"delta":{"content":"partial"}}]}`) + chatFrame(`[DONE]`), "invalid_response"},
		{"bad JSON", chatFrame(`{broken`), "invalid_response"},
		{"bad JSON before done", chatFrame(`{broken`) + chatFrame(`[DONE]`), "invalid_response"},
		{"choice", chatFrame(`{"id":"chat_1","choices":[{"index":1,"delta":{"content":"wrong"}}]}`), "invalid_response"},
		{"identity", chatFrame(`{"id":"chat_1","choices":[]}`) + chatFrame(`{"id":"chat_2","choices":[]}`), "invalid_response"},
		{"filtered", chatFrame(`{"id":"chat_1","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":"content_filter"}]}`) + chatFrame(`[DONE]`), "invalid_response"},
		{"tool index", chatFrame(`{"id":"chat_1","choices":[{"index":0,"delta":{"tool_calls":[{"index":-1}]}}]}`), "invalid_response"},
		{"error", chatFrame(`{"error":{"message":"denied"}}`), "provider_error"},
		{"cache", chatFrame(`{"usage":{"prompt_tokens":1,"prompt_tokens_details":{"cached_tokens":2}}}`), "invalid_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			observation := observeChat(t, test.body, 1)
			if observation.result.ErrorType != test.want || len(observation.result.Messages) > 0 {
				t.Fatalf("result=%+v", observation.result)
			}
		})
	}
}

func TestChatObservationFragmentedToolsAndUsage(t *testing.T) {
	body := chatFrame(`{"id":"chat_1","choices":[{"index":0,"delta":{"content":"running","tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"Read","arguments":"{"}},{"index":0,"id":"call_a","function":{"name":"Bash","arguments":"{\"command\":"}}]}}]}`) +
		chatFrame(`{"id":"chat_1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"pwd\"}"}},{"index":1,"function":{"arguments":"}"}}]},"finish_reason":"tool_calls"}]}`) +
		chatFrame(`{"id":"chat_1","choices":[],"usage":{"prompt_tokens":20,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":12}}}`) + chatFrame(`[DONE]`)
	for _, size := range []int{1, 17, len(body)} {
		observation := observeChat(t, body, size)
		result := observation.result
		if result.ErrorType != "" || !observation.complete || observation.stopReason != "tool_use" || len(result.ToolUses) != 2 || len(result.Messages) != 1 {
			t.Fatalf("result=%+v", result)
		}
		if *result.Usage.InputTokens != 8 || *result.Usage.OutputTokens != 5 || *result.Usage.CacheReadInputTokens != 12 {
			t.Fatalf("usage=%+v", result.Usage)
		}
		if result.Messages[0].ID != maevents.StableAssistantEventID("cse_chat", "chat_1", 0, "agent.message") {
			t.Fatal("message identity differs")
		}
		if result.ToolUses[0].ID != "call_b" || string(result.ToolUses[1].Input) != `{"command":"pwd"}` || result.ToolUses[1].ID != "call_a" {
			t.Fatalf("tools=%+v", result.ToolUses)
		}
	}
}

func TestChatObservationMapsFinishReasonToStopReason(t *testing.T) {
	for _, test := range []struct{ finish, want string }{{"stop", "end_turn"}, {"length", "max_tokens"}} {
		body := chatFrame(`{"id":"chat_1","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"`+test.finish+`"}]}`) + chatFrame(`[DONE]`)
		observation := observeChat(t, body, len(body))
		if !observation.complete || observation.stopReason != test.want || modelStopReasons.Normalize(observation.stopReason) != test.want {
			t.Fatalf("finish=%s stop=%q", test.finish, observation.stopReason)
		}
	}
}

func TestChatObservationMissingUsageAndEmptyArguments(t *testing.T) {
	body := chatFrame(`{"id":"chat_empty_args","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call","function":{"name":"Status"}}]},"finish_reason":"tool_calls"}]}`) + chatFrame(`[DONE]`)
	observation := observeChat(t, body, len(body))
	if observation.result.ErrorType != "" || observation.result.Usage.InputTokens != nil || string(observation.result.ToolUses[0].Input) != "{}" {
		t.Fatalf("result=%+v", observation.result)
	}
	for _, body := range []string{`{"stream":true}`, `{"stream":true,"n":1}`} {
		if err := validateChatRequest([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}

func observeChat(t *testing.T, body string, size int) *responseObservation {
	t.Helper()
	observation := &responseObservation{chat: &chatObservation{}, streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_chat"}}
	for offset := 0; offset < len(body); offset += size {
		_, _ = observation.Write([]byte(body[offset:min(offset+size, len(body))]))
	}
	observation.finish()
	return observation
}

func chatFrame(data string) string {
	return "data: " + strings.ReplaceAll(data, "\n", "\ndata: ") + "\r\n\r\n"
}

func TestChatObservationBlockIdentity(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
		t.Run(field, func(t *testing.T) {
			body := chatFrame(`{"id":"chat_order","choices":[{"index":0,"delta":{"tool_calls":[{"index":9,"id":"call","function":{"name":"Bash","arguments":"{}"}}]}}]}`) +
				chatFrame(`{"id":"chat_order","choices":[{"index":0,"delta":{"`+field+`":"thinking"}}]}`) +
				chatFrame(`{"id":"chat_order","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"tool_calls"}]}`) + chatFrame(`[DONE]`)
			observation := observeChat(t, body, 1)
			result := observation.result
			if result.ErrorType != "" || len(result.Messages) != 2 {
				t.Fatalf("result=%+v", result)
			}
			for index, kind := range []string{"agent.thinking", "agent.message"} {
				if result.Messages[index].ID != maevents.StableAssistantEventID("cse_chat", "chat_order", index+1, kind) {
					t.Fatalf("message=%+v", result.Messages[index])
				}
			}
			if len(result.Messages[0].Content) != 0 {
				t.Fatalf("thinking=%+v", result.Messages[0])
			}
		})
	}
}

func TestChatObservationReasoningPrecedesContentInSameFrame(t *testing.T) {
	for _, delta := range []string{
		`{"content":"answer","reasoning_content":"thinking"}`,
		`{"content":"answer","reasoning_content":"","reasoning":"thinking"}`,
		`{"content":"answer","reasoning_text":"thinking"}`,
	} {
		t.Run(delta, func(t *testing.T) {
			body := chatFrame(`{"id":"chat_mixed","choices":[{"index":0,"delta":`+delta+`,"finish_reason":"stop"}]}`) + chatFrame(`[DONE]`)
			result := observeChat(t, body, len(body)).result
			if result.ErrorType != "" || len(result.Messages) != 2 {
				t.Fatalf("result=%+v", result)
			}
			for index, kind := range []string{"agent.thinking", "agent.message"} {
				if result.Messages[index].ID != maevents.StableAssistantEventID("cse_chat", "chat_mixed", index, kind) {
					t.Fatalf("message=%+v", result.Messages[index])
				}
			}
		})
	}
}

func TestChatObservationNumbersToolsWhenTheirNameArrives(t *testing.T) {
	body := chatFrame(`{"id":"chat_late","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"arguments":"{\"a\":"}}]}}]}`) +
		chatFrame(`{"id":"chat_late","choices":[{"index":0,"delta":{"content":"text"}}]}`) +
		chatFrame(`{"id":"chat_late","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"Bash","arguments":"1}"}}]},"finish_reason":"tool_calls"}]}`) + chatFrame(`[DONE]`)
	result := observeChat(t, body, 1).result
	if result.ErrorType != "" || len(result.Messages) != 1 || len(result.ToolUses) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if result.Messages[0].ID != maevents.StableAssistantEventID("cse_chat", "chat_late", 0, "agent.message") {
		t.Fatalf("message=%+v", result.Messages[0])
	}
	if result.ToolUses[0].ID != "call_1" || string(result.ToolUses[0].Input) != `{"a":1}` {
		t.Fatalf("tools=%+v", result.ToolUses)
	}
	unnamed := chatFrame(`{"id":"chat_unnamed","choices":[{"index":0,"delta":{"content":"text","tool_calls":[{"index":0,"id":"call_1","function":{"arguments":"{}"}}]},"finish_reason":"stop"}]}`) + chatFrame(`[DONE]`)
	if result := observeChat(t, unnamed, len(unnamed)).result; result.ErrorType != "invalid_response" {
		t.Fatalf("unnamed=%+v", result)
	}
}

func TestChatObservationFinishAtEOF(t *testing.T) {
	body := chatFrame(`{"id":"chat_eof","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`)
	observation := observeChat(t, body, 1)
	if !observation.complete || observation.result.ErrorType != "" || len(observation.result.Messages) != 1 {
		t.Fatalf("result=%+v", observation.result)
	}
}

func TestChatObservationEOFFinalizerDoesNotReenter(t *testing.T) {
	observation := &responseObservation{chat: &chatObservation{}, streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_chat"}}
	completed := make(chan struct{})
	observation.onComplete = sync.OnceFunc(func() { observation.finish(); close(completed) })
	_, _ = observation.Write([]byte(chatFrame(`{"id":"chat_eof","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`)))
	go observation.onComplete()
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("model request finalizer reentered")
	}
	if observation.result.ErrorType != "" || !observation.complete {
		t.Fatalf("result=%+v", observation.result)
	}
}

func TestChatObservationEOFDoesNotMaskTransportFailure(t *testing.T) {
	observation := &responseObservation{chat: &chatObservation{}, streaming: true, request: &codesessions.ModelRequest{CodeSessionID: "cse_chat"}}
	_, _ = observation.Write([]byte(chatFrame(`{"id":"chat_eof","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`)))
	observation.result.ErrorType = "stream_error"
	observation.finish()
	if observation.complete || observation.result.ErrorType != "stream_error" || len(observation.result.Messages) != 0 {
		t.Fatalf("result=%+v", observation.result)
	}
}
