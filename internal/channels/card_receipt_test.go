package channels

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func seedCardActionReceipt(worker *channelWorker, inputID string) {
	store := worker.supervisor.store.(*fakeRuntimeStore)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.deliveries == nil {
		store.deliveries = map[string]db.ChannelReplyDelivery{}
	}
	store.deliveries["chnl_1:"+inputID] = db.ChannelReplyDelivery{ChannelExternalID: "chnl_1", ChannelType: "feishu", AccountID: "app_1", InputEventID: inputID, SessionExternalID: "sesn_1", ScopeKey: "chat:oc_1", ChatID: "oc_1", ChatType: "p2p", ActorID: "ou_1", ReplyMessageID: "om_card", State: "sent"}
}

func TestCardActionRejectsUnrelatedPlatformReceiptBeforeOMA(t *testing.T) {
	for _, kind := range []string{"缺少消息ID", "转发卡片", "其他渠道", "其他应用", "其他会话", "其他作用域", "其他聊天", "其他操作者", "已完成卡片"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusOK) }))
			defer server.Close()
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
			worker, _ := testWorker(t, store, server.URL)
			seedCardActionReceipt(worker, "card_input")
			event := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{"kind": "stop", "channel_id": "chnl_1", "scope_key": "chat:oc_1", "session_id": "sesn_1", "tool_use_id": "input", "open_id": "ou_1"}}}
			row := store.deliveries["chnl_1:card_input"]
			switch kind {
			case "缺少消息ID":
				event.MessageID = ""
			case "转发卡片":
				event.MessageID = "om_forwarded"
			case "其他渠道":
				row.ChannelExternalID = "other_channel"
			case "其他应用":
				row.AccountID = "other_app"
			case "其他会话":
				row.SessionExternalID = "other_session"
			case "其他作用域":
				row.ScopeKey = "thread:oc_1:other"
			case "其他聊天":
				row.ChatID = "other_chat"
			case "其他操作者":
				row.ActorID = "other_actor"
			case "已完成卡片":
				row.State = "completed"
			}
			store.deliveries["chnl_1:card_input"] = row
			if err := worker.handleCardAction(t.Context(), event); err == nil || requests.Load() != 0 {
				t.Fatalf("卡片回执不匹配必须在OMA读写前拒绝: kind=%s err=%v requests=%d", kind, err, requests.Load())
			}
		})
	}
}
