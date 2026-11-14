package channels

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestConversationCardKeepsMigratedStructure(t *testing.T) {
	reply := channelReply{thinking: true}
	record := db.ChannelRecord{IncludeThinking: true, IncludeToolCalls: true}
	reply.consume(record, omaEvent{ID: "evt_tool", Type: "agent.tool_use", Name: "bash", Input: []byte(`{"command":"printf identity_after_restart_852"}`)})
	var running struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
			Template string `json:"template"`
		} `json:"header"`
	}
	if err := json.Unmarshal([]byte(reply.render()), &running); err != nil {
		t.Fatal(err)
	}
	if running.Header.Template != "blue" || running.Header.Title.Content != "正在调用工具" {
		t.Fatalf("运行卡片缺少旧版状态标题: %s", reply.render())
	}
	reply.consume(record, omaEvent{Type: "agent.tool_result", ToolUseID: "evt_tool", Content: []byte(`[{"type":"text","text":"identity_after_restart_852"}]`)})
	reply.consume(record, omaEvent{Type: "agent.message", Content: []byte(`[{"type":"text","text":"工具已返回。"}]`)})
	reply.consume(record, omaEvent{Type: "session.status_idle", StopReason: []byte(`{"type":"end_turn"}`)})
	got := reply.render()
	if !strings.Contains(got, `"collapsible_panel"`) || !strings.Contains(got, "identity_after_restart_852") || !strings.Contains(got, "已完成") || strings.Contains(got, "正在思考") {
		t.Fatalf("完成卡片必须保留状态和可展开工具面板: %s", got)
	}
}

func TestOrdinaryConversationUsesCardWhenThinkingIsHidden(t *testing.T) {
	worker, fake := testWorker(t, &fakeRuntimeStore{}, "")
	worker.record.IncludeThinking = false
	stream, err := worker.adapter.openReply(t.Context(), &channelMessage{ChatID: "oc_1"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.close(context.Background())
	if err := stream.update(t.Context(), &channelReply{}); err != nil {
		t.Fatal(err)
	}
	if len(fake.inputs) != 1 || fake.inputs[0].Card == "" || fake.inputs[0].Markdown != "" {
		t.Fatalf("思考开关不能切换普通回复格式: %+v", fake.inputs)
	}
}

func TestRunningCardHasStopButtonAndFinalCardRemovesIt(t *testing.T) {
	reply := channelReply{stopValue: &channelActionValue{Kind: "stop", ChannelID: "chnl_1", ScopeKey: "chat:oc_1", SessionID: "sesn_1", ToolUseID: "evt_input", OpenID: "ou_1"}}
	if !strings.Contains(reply.render(), "停止对话") {
		t.Fatal("运行卡片缺少停止按钮")
	}
	reply.consume(db.ChannelRecord{}, omaEvent{Type: "session.status_idle", StopReason: []byte(`{"type":"end_turn"}`)})
	if strings.Contains(reply.render(), "停止对话") {
		t.Fatal("已结束卡片不能继续显示停止按钮")
	}
}

func TestUserInterruptOverridesUpstreamEndTurn(t *testing.T) {
	reply := channelReply{stopValue: &channelActionValue{Kind: "stop"}}
	reply.consume(db.ChannelRecord{}, omaEvent{Type: "user.interrupt"})
	if !strings.Contains(reply.render(), "正在停止") || strings.Contains(reply.render(), "停止对话") {
		t.Fatalf("中断请求发出后应等待确认并移除按钮: %s", reply.render())
	}
	reply.consume(db.ChannelRecord{}, omaEvent{Type: "session.status_idle", StopReason: []byte(`{"type":"end_turn"}`)})
	if !strings.Contains(reply.render(), "已停止") || strings.Contains(reply.render(), "已完成") {
		t.Fatalf("真实中断不能被上游 end_turn 覆盖: %s", reply.render())
	}
}
