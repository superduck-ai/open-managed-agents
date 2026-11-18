package feishu

import "testing"

func TestFeishuWSClientRegistersEventDispatcher(t *testing.T) {
	client := newWSClient("cli_test", "secret")
	if client.EventHandler() == nil {
		t.Fatal("飞书长连接必须配置事件分发器，否则 OnMessage / OnCardAction 注册的处理器永远不会被调用")
	}
}
