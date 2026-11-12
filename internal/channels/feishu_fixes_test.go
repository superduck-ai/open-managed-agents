package channels

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestChannelThreadIDReadsSDKStructEvent(t *testing.T) {
	var raw larkim.P2MessageReceiveV1
	payload := `{"schema":"2.0","header":{"event_id":"evt_1"},"event":{"message":{"message_id":"om_1","thread_id":"omt_1"}}}`
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", RawEvent: &raw}
	if thread := channelThreadID(msg); thread != "omt_1" {
		t.Fatalf("话题 ID = %q，期望 omt_1", thread)
	}
	if scope := channelScopeKey(msg); scope != "thread:oc_1:omt_1" {
		t.Fatalf("scopeKey = %q", scope)
	}
}

func TestReplyPollingDoesNotReplayEarlierTurn(t *testing.T) {
	var polls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/events") || r.Method != http.MethodGet {
			_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
			return
		}
		events := `{"data":[
			{"id":"evt_old_agent","type":"agent.message","content":[{"type":"text","text":"旧回复"}]},
			{"id":"evt_old_idle","type":"session.status_idle","stop_reason":{"type":"end_turn"}},
			{"id":"evt_input","type":"user.message","content":[{"type":"text","text":"新问题"}]}
		]}`
		if polls.Add(1) > 1 {
			events = `{"data":[
				{"id":"evt_old_agent","type":"agent.message","content":[{"type":"text","text":"旧回复"}]},
				{"id":"evt_old_idle","type":"session.status_idle","stop_reason":{"type":"end_turn"}},
				{"id":"evt_input","type":"user.message","content":[{"type":"text","text":"新问题"}]},
				{"id":"evt_new_agent","type":"agent.message","content":[{"type":"text","text":"新回复"}]},
				{"id":"evt_new_idle","type":"session.status_idle","stop_reason":{"type":"end_turn"}}
			]}`
		}
		_, _ = w.Write([]byte(events))
	}))
	t.Cleanup(server.Close)
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	task := &channelReplyTask{ctx: context.Background(), scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "evt_input", inbound: map[string]struct{}{}}
	store := worker.supervisor.store.(*fakeRuntimeStore)
	store.mu.Lock()
	store.bindings["chat:oc_1"] = "sesn_1"
	store.mu.Unlock()
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}
	if err := worker.streamReply(context.Background(), task, msg, "key"); err != nil {
		t.Fatalf("streamReply: %v", err)
	}
	channel.mu.Lock()
	appends := strings.Join(channel.appends, "")
	channel.mu.Unlock()
	if strings.Contains(appends, "旧回复") {
		t.Fatalf("不应重放上一轮事件: %q", appends)
	}
	if !strings.Contains(appends, "新回复") {
		t.Fatalf("应输出本轮回复: %q", appends)
	}
}

func TestStreamReplyStopsWhenBindingReplaced(t *testing.T) {
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/events") && r.Method == http.MethodGet {
			select {
			case <-stopped:
			default:
				close(stopped)
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"evt_input","type":"user.message"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"sesn_1","status":"idle"}`))
	}))
	t.Cleanup(server.Close)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	task := &channelReplyTask{ctx: context.Background(), scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "evt_input"}
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}
	if err := worker.streamReply(context.Background(), task, msg, "key"); err != nil {
		t.Fatalf("streamReply: %v", err)
	}
}

func TestMCPToolApprovalSendsCard(t *testing.T) {
	worker, channel := testWorker(t, &fakeRuntimeStore{}, "")
	event := omaEvent{ID: "evt_mcp_1", Type: "agent.mcp_tool_use", Name: "search", Permission: "ask", MCPToolUseID: "evt_mcp_1", Input: json.RawMessage(`{"q":"hi"}`)}
	task := &channelReplyTask{ctx: context.Background(), scopeKey: "chat:oc_1", sessionID: "sesn_1"}
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}
	if err := worker.askIfNeeded(context.Background(), msg, task, event, map[string]struct{}{}); err != nil {
		t.Fatalf("askIfNeeded: %v", err)
	}
	channel.mu.Lock()
	inputs := append([]*channeltypes.SendInput{}, channel.inputs...)
	channel.mu.Unlock()
	if len(inputs) != 1 || !strings.Contains(inputs[0].Card, "search") {
		t.Fatalf("MCP 工具审批应发送卡片: %+v", inputs)
	}
	if err := worker.askIfNeeded(context.Background(), msg, task, event, map[string]struct{}{"evt_mcp_1": {}}); err != nil {
		t.Fatalf("askIfNeeded: %v", err)
	}
	channel.mu.Lock()
	count := len(channel.inputs)
	channel.mu.Unlock()
	if count != 1 {
		t.Fatalf("已回答的工具不应重复发卡片: %d", count)
	}
}

func TestQuestionCardUsesSingleSelectAndAnswers(t *testing.T) {
	value := channelActionValue{Kind: "answer", SessionID: "sesn_1", ToolUseID: "evt_q1"}
	questions := []channelQuestion{
		{ID: "q1", Title: "单选", Options: []string{"A", "B"}},
		{ID: "q2", Title: "多选", MultiSelect: true, Options: []string{"A", "B"}},
	}
	card := questionCard(value, questions)
	if !strings.Contains(card, `"tag":"select_static"`) || !strings.Contains(card, `"tag":"multi_select_static"`) {
		t.Fatalf("单选/多选控件不正确: %s", card)
	}
	form := map[string]any{"q1": []any{"A", "B"}, "q1_custom": "", "q2": []any{"A", "B"}, "q3_custom": "自定义答案"}
	decoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed channelActionValue
	if err := json.Unmarshal(decoded, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	parsed.Multi = []string{"q2"}
	answers := buildAnswers(parsed, form)
	byID := make(map[string]map[string]any, len(answers))
	for _, answer := range answers {
		byID[answer["id"].(string)] = answer
	}
	if selected := byID["q1"]["selected"].([]string); len(selected) != 1 || selected[0] != "A" {
		t.Fatalf("单选应只保留一个: %v", byID["q1"])
	}
	if selected := byID["q2"]["selected"].([]string); len(selected) != 2 {
		t.Fatalf("多选应保留全部: %v", byID["q2"])
	}
	if byID["q3"]["custom"] != "自定义答案" {
		t.Fatalf("纯自定义答案丢失: %v", answers)
	}
	if selected := byID["q3"]["selected"].([]string); len(selected) != 0 {
		t.Fatalf("纯自定义答案的 selected 应为空: %v", byID["q3"])
	}
}

func TestCommandsRequirePairingAuthorization(t *testing.T) {
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, "")
	worker.record.Mode = "pairing"
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1", Content: "/new"}
	if err := worker.handleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handleMessage: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	store.mu.Lock()
	deleted := append([]string{}, store.deleted...)
	_, bound := store.bindings["chat:oc_1"]
	store.mu.Unlock()
	if len(deleted) != 0 || !bound {
		t.Fatalf("未授权用户不应能重置会话: deleted=%v bound=%v", deleted, bound)
	}
}

func TestRegistrationAddonsSeparateCallbacks(t *testing.T) {
	addons := channelAppAddons()
	if contains(addons.Events.Items.Tenant, "card.action.trigger") {
		t.Fatal("card.action.trigger 不应放在事件订阅里")
	}
	if !contains(addons.Callbacks.Items, "card.action.trigger") {
		t.Fatal("card.action.trigger 应注册为回调")
	}
	if !contains(addons.Events.Items.Tenant, "im.message.receive_v1") {
		t.Fatal("消息事件订阅缺失")
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
