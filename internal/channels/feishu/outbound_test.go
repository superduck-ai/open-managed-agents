package feishu

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

func TestReplyMessageRequestsThreadReply(t *testing.T) {
	var replyBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
			_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"token","expire":7200}`))
		case r.URL.Path == "/open-apis/im/v1/messages/om_anchor/reply":
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
	stream, err := adapter.OpenReply(t.Context(), &contracts.Message{ChatType: "p2p", MessageID: "om_input", ThreadReplyTo: "om_anchor"}, contracts.Reply{DeliveryKey: "oauth_key"})
	if err != nil {
		t.Fatal(err)
	}
	if stream.Receipt() != "om_2" {
		t.Fatalf("回复消息 ID = %q", stream.Receipt())
	}
	if replyBody["reply_in_thread"] != true || replyBody["uuid"] != "oauth_key" || replyBody["msg_type"] != "interactive" {
		t.Fatalf("话题卡片必须使用话题回复并透传去重键，实际 %v", replyBody)
	}
}
