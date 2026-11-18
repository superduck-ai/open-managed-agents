package feishu

import (
	"context"
	"encoding/json"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplyMessageRequestsThreadReply(t *testing.T) {
	var replyBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token","expire":7200}`))
		case strings.Contains(r.URL.Path, "/im/v1/messages/") && strings.HasSuffix(r.URL.Path, "/reply"):
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &replyBody); err != nil {
				t.Errorf("解析回复请求: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_2"}}`))
		default:
			t.Errorf("未预期的请求 %s", r.URL.Path)
			_, _ = w.Write([]byte(`{"code":0}`))
		}
	}))
	defer server.Close()
	adapter := &Adapter{Client: lark.NewClient("cli_test", "secret", lark.WithOpenBaseUrl(server.URL)), Config: Config{ReplyInThread: true}}
	messageID, err := adapter.replyMessage(context.Background(), "om_1", "text", `{"text":"好的"}`)
	if err != nil {
		t.Fatalf("replyMessage: %v", err)
	}
	if messageID != "om_2" {
		t.Fatalf("回复消息 ID = %q", messageID)
	}
	if replyBody["reply_in_thread"] != true {
		t.Fatalf("回复请求必须带 reply_in_thread=true，实际 %v", replyBody)
	}
}
