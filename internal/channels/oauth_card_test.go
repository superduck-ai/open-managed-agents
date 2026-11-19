package channels

import (
	"encoding/json/v2"
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
