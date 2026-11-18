package sessiontitles

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestRejectUnusableTitleCompletion(t *testing.T) {
	for _, tc := range []struct{ backend, payload string }{
		{"", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"Partial"}]}`},
		{"", `{"stop_reason":"tool_use","content":[{"type":"tool_use"}]}`},
		{"", `{"stop_reason":"end_turn","content":[{"type":"text","text":"  "}]}`},
		{"", `{"stop_reason":"end_turn","content":[{"type":"text","text":"title"},{"type":"tool_use"}]}`},
		{llmproviders.ChatCompletions, `{"choices":[{"finish_reason":"length","message":{"content":"Partial"}}]}`},
		{llmproviders.ChatCompletions, `{"choices":[{"finish_reason":"stop","message":{"content":"Title","tool_calls":[{}]}}]}`},
	} {
		if title, err := parseTitle(tc.backend, []byte(tc.payload)); err == nil || title != "" {
			t.Fatalf("accepted %s", tc.payload)
		}
	}
}

func TestIndependentTitleRequest(t *testing.T) {
	for _, backend := range []string{llmproviders.Messages, llmproviders.ChatCompletions} {
		t.Run(backend, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request modelRequest
				if err := json.UnmarshalRead(r.Body, &request); err != nil {
					t.Fatal(err)
				}
				if request.Model != "test-model" || request.MaxTokens != 128 {
					t.Fatalf("request=%+v", request)
				}
				last := request.Messages[len(request.Messages)-1]
				if last.Role != "user" || !strings.HasSuffix(last.Content, `["修复 session 标题"]`) {
					t.Fatalf("input=%s", last.Content)
				}
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Fatal("missing provider credentials")
				}
				if r.UserAgent() != "OMA-Session-Title" {
					t.Fatalf("unexpected title request user agent %q", r.UserAgent())
				}
				if backend == llmproviders.ChatCompletions {
					if r.URL.Path != "/v1/chat/completions" || request.Messages[0].Role != "system" {
						t.Fatal("bad chat request")
					}
					w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"**修复会话标题**\nexplanation"}}]}`))
				} else {
					if r.URL.Path != "/v1/messages" || request.System == "" || len(request.Messages) != 1 {
						t.Fatal("bad messages request")
					}
					w.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"**修复会话标题**\nexplanation"}]}`))
				}
			}))
			defer upstream.Close()
			m := &model{client: upstream.Client()}
			title, err := m.complete(t.Context(), llmproviders.Upstream{APIBackend: backend, BaseURL: upstream.URL, APIKey: "test-key", ModelID: "test-model"}, "修复 session 标题")
			if err != nil || title != "修复会话标题" {
				t.Fatalf("%q %v", title, err)
			}
		})
	}
}
