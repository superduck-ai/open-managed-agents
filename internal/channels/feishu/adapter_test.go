package feishu

import (
	"context"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
)

func TestFeishuWSClientRegistersEventDispatcher(t *testing.T) {
	client := newWSClient("cli_test", "secret")
	if client.EventHandler() == nil {
		t.Fatal("飞书长连接必须配置事件分发器，否则 OnMessage / OnCardAction 注册的处理器永远不会被调用")
	}
}

func TestFeishuNewSessionMenuUsesClickingActor(t *testing.T) {
	for _, tc := range []struct {
		name, appID, key, openID string
		want                     bool
	}{
		{name: "其他应用", appID: "cli_other", key: "chat_new", openID: "ou_user"},
		{name: "其他菜单", appID: "cli_test", key: "report_daily", openID: "ou_user"},
		{name: "缺少点击者", appID: "cli_test", key: "chat_new"},
		{name: "新建会话", appID: "cli_test", key: "chat_new", openID: "ou_user", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			adapter := &Adapter{Config: Config{AppID: "cli_test", NewSession: func(_ context.Context, eventID, actorID string) error {
				called = true
				if eventID != "event_1" || actorID != "ou_user" {
					t.Fatalf("菜单事件标识不匹配: %q %q", eventID, actorID)
				}
				return nil
			}}}
			event := &larkapplication.P2BotMenuV6{
				EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{AppID: tc.appID, EventID: "event_1"}},
				Event: &larkapplication.P2BotMenuV6Data{
					EventKey: new(tc.key),
					Operator: &larkapplication.Operator{OperatorId: &larkapplication.UserId{OpenId: new(tc.openID)}},
				},
			}
			if err := adapter.receiveMenuEvent(t.Context(), event); err != nil || called != tc.want {
				t.Fatalf("菜单分发结果错误: %v %v", called, err)
			}
		})
	}
}
