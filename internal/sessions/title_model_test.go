package sessions

import (
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
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
			input.Model != "deepseek-flash" || input.MaxTokens != titleOutputTokens ||
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
