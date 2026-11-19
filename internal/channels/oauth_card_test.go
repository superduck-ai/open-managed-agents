package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/channels/feishu"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestOAuthCardRequiresPrivateConversationAndTrustedOrigin(t *testing.T) {
	event := omaEvent{ID: "evnt_oauth", Type: "agent.custom_tool_use", Name: "RequestOAuth", Input: []byte(`{"kind":"env_oauth","provider":"custom:friday","secret_name":"FRIDAY_4A_TOKEN","parent_call_id":"bash_1","reason":"missing","authorization_url":"https://untrusted.example"}`)}
	reply := channelReply{oauthOrigin: "https://oma.example", stopValue: &channelActionValue{ChannelID: "channel_1", SessionID: "session_1", ChatType: "group"}}
	if !reply.consumeInteraction(event) {
		t.Fatal("应保留 OAuth 交互")
	}
	data, err := json.Marshal(reply.interactionElements())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "open_url") || !strings.Contains(string(data), "私聊") {
		t.Fatal("群聊不得提供个人授权链接")
	}
	reply.stopValue.ChatType = "p2p"
	data, err = json.Marshal(reply.interactionElements())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "untrusted.example") || !strings.Contains(string(data), "https://oma.example/oauth/channels/channel_1/sessions/session_1/requests/evnt_oauth") {
		t.Fatal("OAuth 入口必须由渠道配置和实际请求生成")
	}
	var card struct {
		Body struct {
			Elements []struct {
				Tag     string `json:"tag"`
				Columns []struct {
					Elements []struct {
						Tag  string `json:"tag"`
						Text struct {
							Content string `json:"content"`
						} `json:"text"`
					} `json:"elements"`
				} `json:"columns"`
			} `json:"elements"`
		} `json:"body"`
	}
	rendered := reply.render()
	if err := json.Unmarshal([]byte(rendered), &card); err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, element := range card.Body.Elements {
		if element.Tag == "button" {
			t.Fatal("授权卡片按钮应在同一分栏内")
		}
		for _, column := range element.Columns {
			if len(column.Elements) != 1 || column.Elements[0].Tag != "button" {
				t.Fatal("每个授权分栏应只有一个按钮")
			}
			labels = append(labels, column.Elements[0].Text.Content)
		}
	}
	if !slices.Equal(labels, []string{"前往授权", "取消", "停止对话"}) {
		t.Fatalf("授权按钮顺序不符：%v", labels)
	}
	if !reply.consumeInteraction(omaEvent{Type: "user.custom_tool_result", CustomToolUseID: event.ID}) || len(reply.interactions) != 0 {
		t.Fatal("原生结果应清除授权交互")
	}
}

func TestCancelOAuthUsesCanonicalEventThread(t *testing.T) {
	var body struct {
		Events []struct {
			ThreadID string `json:"session_thread_id"`
			IsError  bool   `json:"is_error"`
		} `json:"events"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	request := omaEvent{ID: "oauth", Type: "agent.custom_tool_use", Name: "RequestOAuth", SessionThreadID: "sthr_primary"}
	value := channelActionValue{SessionID: "sesn_1", ToolUseID: "oauth", ThreadID: "sthr_other"}
	if err := worker.cancelOAuthCardAction(t.Context(), "test", value, []omaEvent{request}); err == nil {
		t.Fatal("不同线程的卡片不得提交取消结果")
	}
	value.ThreadID = ""
	if err := worker.cancelOAuthCardAction(t.Context(), "test", value, []omaEvent{request}); err != nil {
		t.Fatal(err)
	}
	if len(body.Events) != 1 || body.Events[0].ThreadID != request.SessionThreadID || !body.Events[0].IsError {
		t.Fatal("实时事件缺少主线程字段时，应使用历史事件的规范线程提交取消")
	}
}

func TestFridayConnectionCardColor(t *testing.T) {
	for status, color := range map[string]string{"等待授权": "blue", "已连接": "green", "已取消授权": "grey", "连接失效": "red"} {
		var card struct {
			Header struct {
				Template string `json:"template"`
				Title    struct {
					Content string `json:"content"`
				} `json:"title"`
			} `json:"header"`
		}
		if err := json.Unmarshal([]byte(feishu.Render(contracts.Reply{Title: "连接 Friday", Status: status})), &card); err != nil {
			t.Fatal(err)
		}
		if card.Header.Template != color || card.Header.Title.Content != "连接 Friday" {
			t.Errorf("%s：表头为 %+v", status, card.Header)
		}
	}
}

func TestSeparateOAuthCardRequiresMatchingReceipt(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused.test")
	seedCardActionReceipt(worker, "input")
	store := worker.supervisor.store.(*fakeRuntimeStore)
	row := store.deliveries["chnl_1:input"]
	row.ArtifactDelivery = `{"oauth_cards":{"oauth_1":"om_auth"}}`
	store.deliveries["chnl_1:input"] = row
	value := channelActionValue{Kind: "cancel_oauth", SessionID: "sesn_1", ScopeKey: "chat:oc_1", SourceID: "om_card", ToolUseID: "oauth_1"}
	for _, id := range []string{"om_auth", "om_card", "om_forwarded"} {
		_, err := worker.cardActionReceipt(t.Context(), value, id, "oc_1", "ou_1")
		if (err == nil) != (id == "om_auth") {
			t.Errorf("授权卡 %s 校验结果 %v", id, err)
		}
	}
	value.Kind, value.ToolUseID = "stop", row.InputEventID
	if _, err := worker.cardActionReceipt(t.Context(), value, "om_auth", "oc_1", "ou_1"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct{ message, input string }{{"om_forwarded", row.InputEventID}, {"om_auth", "other"}} {
		value.ToolUseID = invalid.input
		if _, err := worker.cardActionReceipt(t.Context(), value, invalid.message, "oc_1", "ou_1"); err == nil {
			t.Fatal("错误回执或输入不能停止对话")
		}
	}
}

func TestFridayContinuationKeepsOriginalDeliveryAcrossReplay(t *testing.T) {
	worker, _, _, _, task, msg := artifactDeliveryFixture(t)
	adapter := &independentAdapter{}
	worker.adapter = adapter
	runner := newReplyRunner(worker, task, msg)
	runner.stream = &durableReplyStream{worker: worker, task: task, stream: adapter}
	for range 2 {
		if err := runner.continueAfterOAuth(t.Context(), "oauth_1"); err != nil {
			t.Fatal(err)
		}
	}
	if adapter.opens != 1 || task.delivery.ReplyMessageID != "card" || runner.reply.stopValue.SourceID != "card" || len(adapter.resumed) != 2 || adapter.resumed[0] != adapter.resumed[1] {
		t.Fatal("重放应复用同一张续跑卡并保留原交付")
	}
}

type retryOAuthCardAdapter struct {
	independentAdapter
	last     contracts.Reply
	attempts int
}

func (a *retryOAuthCardAdapter) ResumeReply(string) channelReplyStream { return a }
func (a *retryOAuthCardAdapter) Update(_ context.Context, reply contracts.Reply) error {
	a.attempts++
	if a.attempts == 1 {
		return errors.New("飞书暂时不可用")
	}
	a.last = reply
	return nil
}

func TestNextInputSettlesDetachedOAuthCard(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		worker, _, _, _, task, msg := artifactDeliveryFixture(t)
		task.terminal = false
		adapter := &retryOAuthCardAdapter{}
		worker.adapter = adapter
		runner := newReplyRunner(worker, task, msg)
		runner.stream = &independentAdapter{}
		runner.oauthCards = map[string]string{"oauth": "om_auth"}
		runner.reply.consumeInteraction(omaEvent{ID: "oauth", Type: "agent.custom_tool_use", Name: "RequestOAuth", Input: []byte(`{"kind":"mcp_oauth","mcp_server_name":"notion"}`)})
		next := omaEvent{ID: "next", Type: "user.message", Metadata: []byte(`{"channel_id":"chnl_1","channel_type":"feishu","channel_account_id":"app_1","channel_chat_id":"oc_1","channel_actor_id":"actor","channel_message_id":"next"}`)}
		if done, err := runner.consumeEvent(t.Context(), next); err != nil || done || adapter.attempts != 0 {
			t.Fatalf("未处理的输入不应结束授权：%v %v", done, err)
		}
		if foreign {
			next.Metadata = nil
		} else {
			next.ProcessedAt = []byte(`"2026-11-19T00:00:00Z"`)
		}
		if done, err := runner.consumeEvent(t.Context(), next); err != nil || !done {
			t.Fatalf("收尾失败：%v %v", done, err)
		}
		if adapter.attempts != 2 || !adapter.last.Ended || adapter.last.Status != "已结束授权" || adapter.last.StopValue != nil {
			t.Fatalf("授权卡重试或终态异常：%d %+v", adapter.attempts, adapter.last)
		}
	}
}
