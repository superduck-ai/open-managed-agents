package channels

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type questionNoticeAdapter struct {
	channelAdapter
	message *channelMessage
	text    string
}

func (a *questionNoticeAdapter) replyText(_ context.Context, message *channelMessage, text string) error {
	a.message, a.text = message, text
	return nil
}

func TestQuestionValidationNoticeUsesTrustedChatType(t *testing.T) {
	for _, chatType := range []string{"group", "p2p"} {
		t.Run(chatType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("空答案不能提交控制事件")
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"card_input","type":"user.message"},{"id":"question","type":"agent.custom_tool_use","name":"AskUserQuestion","input":{"questions":[{"id":"q","question":"颜色","options":[{"label":"红"}]}]}}]}`)
			}))
			defer server.Close()
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
			worker, _ := testWorker(t, store, server.URL)
			seedCardActionReceipt(worker, "card_input")
			receipt := store.deliveries["chnl_1:card_input"]
			receipt.ChatType = chatType
			store.deliveries["chnl_1:card_input"] = receipt
			adapter := &questionNoticeAdapter{channelAdapter: worker.adapter}
			worker.adapter = adapter
			event := &channeltypes.CardActionEvent{MessageID: "om_card", ChatID: "oc_1", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{"kind": "answer", "channel_id": "chnl_1", "scope_key": "chat:oc_1", "session_id": "sesn_1", "tool_use_id": "question", "chat_type": "forged"}}}
			if err := worker.handleCardAction(t.Context(), event); err != nil {
				t.Fatal(err)
			}
			if adapter.message == nil || adapter.message.ChatType != chatType || adapter.message.ChatID != receipt.ChatID || adapter.message.MessageID != receipt.ReplyMessageID || adapter.text == "" {
				t.Fatalf("校验提示必须携带可信回执的原聊天与卡片: message=%+v text=%q", adapter.message, adapter.text)
			}
		})
	}
}
