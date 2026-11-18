//go:build integration || e2e || e2b_integration

package liveworker

import (
	"io"
	"net/http"
)

const titleFixtureUserAgent = "OMA-Session-Title"

func withTitleCompletion(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1/messages" && r.UserAgent() == titleFixtureUserAgent {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_chat_title","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"聊天验证"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":4}}`)
			return
		}
		next(w, r)
	}
}
