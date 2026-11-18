package channels

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCardControlRequestKeyUsesPersistedAssociation(t *testing.T) {
	base := db.ChannelReplyDelivery{ChannelType: "feishu", ChannelExternalID: "channel", AccountID: "account", SessionExternalID: "session", InputEventID: "input", ReplyMessageID: "card", ScopeKey: "scope", ChatID: "chat", ActorID: "actor"}
	key := cardControlRequestKey(base, "target")
	if key != cardControlRequestKey(base, "target") || len(key) > 128 {
		t.Fatal("同一原回执必须生成稳定且合法的标识")
	}
	for _, field := range []string{"平台", "渠道", "应用", "会话", "输入", "卡片", "作用域", "聊天", "操作者", "目标"} {
		other := base
		target := "target"
		switch field {
		case "平台":
			other.ChannelType = "other"
		case "渠道":
			other.ChannelExternalID = "other"
		case "应用":
			other.AccountID = "other"
		case "会话":
			other.SessionExternalID = "other"
		case "输入":
			other.InputEventID = "other"
		case "卡片":
			other.ReplyMessageID = "other"
		case "作用域":
			other.ScopeKey = "other"
		case "聊天":
			other.ChatID = "other"
		case "操作者":
			other.ActorID = "other"
		case "目标":
			target = "other"
		}
		if key == cardControlRequestKey(other, target) {
			t.Fatalf("%s不能共用操作标识", field)
		}
	}
}

func TestCardActionsSendStableRequestKey(t *testing.T) {
	for _, kind := range []string{"stop", "confirm", "answer", "cancel_question"} {
		t.Run(kind, func(t *testing.T) {
			target := "tool"
			if kind == "stop" {
				target = "card_input"
			}
			var received string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					received = r.Header.Get("Idempotency-Key")
					_, _ = io.WriteString(w, `{}`)
					return
				}
				if kind == "answer" || kind == "cancel_question" {
					_, _ = io.WriteString(w, `{"data":[{"id":"card_input","type":"user.message"},{"id":"tool","type":"agent.custom_tool_use","name":"AskUserQuestion","input":{"questions":[{"id":"q","question":"颜色","options":[{"label":"红"}]}]}}]}`)
				} else {
					_, _ = io.WriteString(w, `{"data":[{"id":"card_input","type":"user.message"},{"id":"tool","type":"agent.tool_use","evaluated_permission":"ask"}]}`)
				}
			}))
			defer server.Close()
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
			worker, _ := testWorker(t, store, server.URL)
			seedCardActionReceipt(worker, "card_input")
			task := worker.startReplyTask(t.Context(), "chat:oc_1", "sesn_1")
			defer worker.finishReply(task)
			event := &channeltypes.CardActionEvent{MessageID: "om_card", ChatID: "oc_1", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{"kind": kind, "channel_id": "chnl_1", "scope_key": "chat:oc_1", "session_id": "sesn_1", "tool_use_id": target, "result": "allow", "requestKey": "forged"}, FormValue: map[string]any{"q": "红"}}}
			if err := worker.handleCardAction(t.Context(), event); err != nil {
				t.Fatal(err)
			}
			expected := cardControlRequestKey(store.deliveries["chnl_1:card_input"], target)
			if received != expected {
				t.Fatalf("卡片控制输入必须使用可信回执生成的标识: got=%s want=%s", received, expected)
			}
		})
	}
}

func TestControlInputKeepsKeyAfterLostResponseAndClientRecreation(t *testing.T) {
	receipt := db.ChannelReplyDelivery{ChannelType: "feishu", ChannelExternalID: "channel", AccountID: "app", SessionExternalID: "session", InputEventID: "input", ReplyMessageID: "card", ActorID: "actor"}
	key := cardControlRequestKey(receipt, "target")
	var mu sync.Mutex
	var saved []byte
	accepted := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("Idempotency-Key") != key {
			t.Error("输入幂等标识与原回执不一致")
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		if saved != nil && !bytes.Equal(saved, body) {
			mu.Unlock()
			w.WriteHeader(http.StatusConflict)
			return
		}
		first := saved == nil
		if first {
			saved = body
			accepted++
		}
		mu.Unlock()
		if first {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	event := map[string]any{"type": "user.tool_confirmation", "tool_use_id": "target", "result": "allow"}
	if err := newOMAClient(server.URL).sendEvent(t.Context(), "key", "session", event, key); err == nil {
		t.Fatal("首次响应确实丢失，不能误记为已收到响应")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			client := newOMAClient(server.URL)
			if err := client.sendEvent(t.Context(), "key", "session", event, cardControlRequestKey(receipt, "target")); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	changed := map[string]any{"type": "user.tool_confirmation", "tool_use_id": "target", "result": "deny"}
	if err := newOMAClient(server.URL).sendEvent(t.Context(), "key", "session", changed, key); err == nil {
		t.Fatal("相反答案不能覆盖已接受的控制输入")
	}
	mu.Lock()
	defer mu.Unlock()
	if accepted != 1 {
		t.Fatalf("重复回调不能重复接受: %d", accepted)
	}
}
