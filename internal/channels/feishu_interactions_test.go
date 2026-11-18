package channels

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestInteractionStaysInConversationWhenToolDetailsHidden(t *testing.T) {
	for _, event := range []omaEvent{
		{ID: "approval", Type: "agent.mcp_tool_use", Name: "search", Permission: "ask", Input: []byte(`{"q":"验收"}`)},
		{ID: "question", Type: "agent.custom_tool_use", Name: "AskUserQuestion", Input: []byte(`{"questions":[{"id":"color","question":"请选择颜色","options":[{"label":"红色"},{"label":"蓝色"}]}]}`)},
	} {
		t.Run(event.ID, func(t *testing.T) {
			reply := channelReply{stopValue: &channelActionValue{ChannelID: "channel", ScopeKey: "thread:chat:topic", SessionID: "session", OpenID: "user"}}
			_, changed := reply.consume(db.ChannelRecord{}, event)
			card := reply.render()
			if !changed || !strings.Contains(card, `"tool_use_id":"`+event.ID+`"`) || strings.Contains(card, `"tag":"action"`) {
				t.Fatalf("交互必须使用 V2 元素留在原对话卡片，且不受工具展示开关影响: %s", card)
			}
			if event.ID == "question" && !strings.Contains(card, "取消问卷") {
				t.Fatal("问卷缺少取消入口")
			}
			result := omaEvent{Type: "user.tool_confirmation", ToolUseID: event.ID}
			if event.ID == "question" {
				result = omaEvent{Type: "user.custom_tool_result", CustomToolUseID: event.ID}
			}
			reply.consume(db.ChannelRecord{}, result)
			card = reply.render()
			if strings.Contains(card, `"tool_use_id":"`+event.ID+`"`) {
				t.Fatalf("控制台或卡片处理后应移除交互入口: %s", card)
			}
		})
	}
}

func TestQuestionCancelChecksActorAndExistingResponse(t *testing.T) {
	for _, kind := range []string{"他人", "事件读取失败", "已回答", "本人取消"} {
		t.Run(kind, func(t *testing.T) {
			bodies := &omaRecorder{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					bodies.record(string(body))
					_, _ = w.Write([]byte(`{}`))
					return
				}
				if kind == "事件读取失败" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if kind == "已回答" {
					_, _ = w.Write([]byte(`{"data":[{"id":"card_input","type":"user.message"},{"type":"user.custom_tool_result","custom_tool_use_id":"question"}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"card_input","type":"user.message"},{"id":"question","type":"agent.custom_tool_use","name":"AskUserQuestion","session_thread_id":"thread"}]}`))
			}))
			t.Cleanup(server.Close)
			worker, _ := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
			seedCardActionReceipt(worker, "card_input")
			value := map[string]any{"kind": "cancel_question", "channel_id": "chnl_1", "scope_key": "chat:oc_1", "session_id": "sesn_1", "tool_use_id": "question", "open_id": "ou_1", "session_thread_id": "untrusted_thread"}
			event := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: value}}
			if kind == "他人" {
				event.Operator.OpenID = "other"
			}
			err := worker.handleCardAction(t.Context(), event)
			got := strings.Join(bodies.snapshot(), "")
			if kind == "本人取消" {
				if err != nil || !strings.Contains(got, `"is_error":true`) || !strings.Contains(got, "用户取消了问卷") || !strings.Contains(got, `"session_thread_id":"thread"`) {
					t.Fatalf("取消必须发回当前话题的错误结果: %s, %v", got, err)
				}
			} else if got != "" || (kind != "已回答" && err == nil) {
				t.Fatalf("不合法或已回答的操作不得重复写入: %s, %v", got, err)
			}
		})
	}
}

func TestTerminalCardRemovesPendingInteraction(t *testing.T) {
	reply := channelReply{stopValue: &channelActionValue{SessionID: "session"}}
	reply.consume(db.ChannelRecord{}, omaEvent{ID: "approval", Type: "agent.tool_use", Name: "bash", Permission: "ask"})
	reply.consume(db.ChannelRecord{}, omaEvent{Type: "user.interrupt"})
	reply.consume(db.ChannelRecord{}, omaEvent{Type: "session.status_idle", StopReason: []byte(`{"type":"end_turn"}`)})
	if strings.Contains(reply.render(), `"kind":"confirm"`) {
		t.Fatal("中断终态不应保留审批按钮")
	}
}
