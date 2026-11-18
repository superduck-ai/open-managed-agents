//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
)

func withTitleCompletion(stream http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			stream(w, r)
			return
		}
		const maxBodyBytes = 2 << 20
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
		var request struct {
			Stream bool `json:"stream"`
		}
		if err != nil || len(body) > maxBodyBytes || jsonv2.Unmarshal(body, &request) != nil {
			http.Error(w, "invalid model request", http.StatusBadRequest)
			return
		}
		if !request.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_chat_title","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"聊天验证"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":4}}`)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		stream(w, r)
	}
}
