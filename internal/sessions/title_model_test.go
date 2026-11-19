package sessions

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestSessionTitlePromptUsesOnlyText(t *testing.T) {
	event := db.SessionEvent{EventType: "user.message", Payload: []byte(`{"content":[
		{"type":"document","source":{"type":"file","file_id":"file_1"}},
		{"type":"image","source":{"type":"url","url":"https://example.com/photo"}},
		{"type":"text","text":"定位会话延迟"},
		{"type":"text","text":"比较两个客户端"}
	]}`)}
	got, err := sessionTitlePrompt(event)
	if err != nil || got != "定位会话延迟\n比较两个客户端" {
		t.Fatalf("unexpected title prompt %q, %v", got, err)
	}
}

func TestSessionTitleModelResponses(t *testing.T) {
	for _, tc := range []struct {
		name, backend, payload, want string
	}{
		{"tool response", llmproviders.ChatCompletions, `{"choices":[{"finish_reason":"tool_calls","message":{"content":""}}]}`, ""},
		{"truncated response", llmproviders.Messages, `{"stop_reason":"max_tokens","content":[{"type":"text","text":"Partial"}]}`, ""},
		{"reasoning only", llmproviders.Messages, `{"stop_reason":"end_turn","content":[{"type":"thinking","thinking":"private"}]}`, ""},
		{"first line", llmproviders.ChatCompletions, `{"choices":[{"finish_reason":"stop","message":{"content":"\n**排查会话延迟**\n说明"}}]}`, "排查会话延迟"},
		{"join controls", llmproviders.Messages, `{"stop_reason":"end_turn","content":[{"type":"text","text":"نامه\u200cای \u202e👩\u200d💻"}]}`, "نامه\u200cای 👩\u200d💻"},
		{"join controls only", llmproviders.Messages, `{"stop_reason":"end_turn","content":[{"type":"text","text":"\u200c\u200d"}]}`, ""},
		{"complete text", llmproviders.Messages, `{"stop_reason":"end_turn","content":[{"type":"text","text":"\u202e Diagnose\tlatency"}]}`, "Diagnose latency"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readSessionTitle(strings.NewReader(tc.payload), tc.backend)
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestGenerateTitleUsingChatCompletions(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var input titleCompletionRequest
		if err := json.UnmarshalRead(req.Body, &input); err != nil {
			t.Error(err)
		}
		if req.URL.Path != "/v1/chat/completions" || req.Header.Get("Authorization") != "Bearer fixture-key" ||
			input.Model != "deepseek-flash" || input.MaxTokens != titleOutputTokens || input.MaxCompletionTokens != 0 ||
			input.Thinking == nil || input.Thinking.Type != "disabled" ||
			len(input.Messages) != 2 || input.Messages[0] != (titleMessage{Role: "system", Content: titleSystemPrompt}) ||
			input.Messages[1] != (titleMessage{Role: "user", Content: "Help diagnose the delay"}) {
			t.Error("unexpected title request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"Diagnose session delay"}}]}`)
	}))
	t.Cleanup(model.Close)
	got, err := generateSessionTitle(t.Context(), model.Client(), llmproviders.Upstream{
		APIBackend: llmproviders.ChatCompletions, BaseURL: model.URL, APIKey: "fixture-key", ModelID: "deepseek-flash",
	}, "", "Help diagnose the delay")
	if err != nil || got != "Diagnose session delay" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestGenerateTitleUsingOpenAICompletionTokens(t *testing.T) {
	for _, test := range []struct {
		modelID string
		effort  string
	}{
		{"o3-mini", ""},
		{"gpt-5", "low"},
		{"gpt-5-mini", "low"},
		{"openai/gpt-5.1", "low"},
		{"gpt-6-astra", "low"},
		{"gpt-5-chat-latest", ""},
	} {
		t.Run(test.modelID, func(t *testing.T) {
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var limits struct {
					MaxTokens           *int   `json:"max_tokens"`
					MaxCompletionTokens int    `json:"max_completion_tokens"`
					ReasoningEffort     string `json:"reasoning_effort"`
				}
				if err := json.UnmarshalRead(req.Body, &limits); err != nil || limits.MaxTokens != nil ||
					limits.MaxCompletionTokens != titleReasoningTokens || limits.ReasoningEffort != test.effort {
					t.Errorf("unexpected title request limits: %+v, %v", limits, err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"Diagnose session delay"}}]}`)
			}))
			t.Cleanup(model.Close)
			got, err := generateSessionTitle(t.Context(), model.Client(), llmproviders.Upstream{
				APIBackend: llmproviders.ChatCompletions, BaseURL: model.URL, ModelID: test.modelID,
			}, "", "Help diagnose the delay")
			if err != nil || got != "Diagnose session delay" {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestTitleModelRequestErrorOmitsProviderAddress(t *testing.T) {
	got := titleModelRequestError(&url.Error{
		Op: "Post", URL: "https://private-provider.example/v1/messages?key=private",
		Err: &net.DNSError{Err: "no such host", Name: "private-provider.example"},
	})
	if got.Error() != "title model DNS lookup failed" {
		t.Fatalf("unexpected model failure: %v", got)
	}
}

func TestGenerateTitlePreservesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	model := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		cancel(errors.New("worker shutdown"))
		<-req.Context().Done()
	}))
	t.Cleanup(model.Close)
	_, err := generateSessionTitle(ctx, model.Client(), llmproviders.Upstream{
		APIBackend: llmproviders.Messages, BaseURL: model.URL, ModelID: "fixture",
	}, "", "Help diagnose the delay")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled title request returned %v", err)
	}
}
