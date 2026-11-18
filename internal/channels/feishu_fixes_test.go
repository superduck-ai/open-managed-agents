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
	if thread := feishuMessage(msg).ThreadID; thread != "omt_1" {
		t.Fatalf("话题 ID = %q，期望 omt_1", thread)
	}
	if scope := channelScopeKey(feishuMessage(msg)); scope != "thread:oc_1:omt_1" {
		t.Fatalf("scopeKey = %q", scope)
	}
}

func TestReplySubscriptionDoesNotQueryHistory(t *testing.T) {
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
	worker.supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent {
		return []omaEvent{
			{ID: "evt_input", Type: "user.message"},
			{ID: "evt_new_agent", Type: "agent.message", Content: []byte(`"新回复"`)},
			{ID: "evt_new_idle", Type: "session.status_idle", StopReason: []byte(`"end_turn"`)},
		}
	})
	task := &channelReplyTask{ctx: context.Background(), scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "evt_input", inbound: map[string]struct{}{}}
	store := worker.supervisor.store.(*fakeRuntimeStore)
	store.mu.Lock()
	store.bindings["chat:oc_1"] = "sesn_1"
	store.mu.Unlock()
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1"}
	if err := worker.streamReply(context.Background(), task, feishuMessage(msg), "key"); err != nil {
		t.Fatalf("streamReply: %v", err)
	}
	channel.mu.Lock()
	appends := strings.Join(channel.appends, "")
	channel.mu.Unlock()
	if polls.Load() != 0 {
		t.Fatalf("内部订阅不应查询历史HTTP接口: %d", polls.Load())
	}
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
	if err := worker.streamReply(context.Background(), task, feishuMessage(msg), "key"); err != nil {
		t.Fatalf("streamReply: %v", err)
	}
}

func TestQuestionCardUsesSingleSelectAndAnswers(t *testing.T) {
	value := channelActionValue{Kind: "answer", SessionID: "sesn_1", ToolUseID: "evt_q1"}
	questions := []channelQuestion{
		{ID: "q1", Title: "单选", Options: []channelQuestionOption{{Label: "A"}, {Label: "B"}}},
		{ID: "q2", Title: "多选", MultiSelect: true, Options: []channelQuestionOption{{Label: "A"}, {Label: "B"}}},
		{ID: "q3", Title: "自定义"},
	}
	encodedCard, err := json.Marshal(questionElements(value, questions))
	if err != nil {
		t.Fatal(err)
	}
	card := string(encodedCard)
	if !strings.Contains(card, `"tag":"select_static"`) || !strings.Contains(card, `"tag":"multi_select_static"`) {
		t.Fatalf("单选/多选控件不正确: %s", card)
	}
	form := map[string]any{"q1": []any{"A"}, "q1_custom": "", "q2": []any{"A", "B"}, "q3_custom": "自定义答案"}
	answers, err := buildAnswers(questions, form)
	if err != nil {
		t.Fatal(err)
	}
	if len(answers[0].Selected) != 1 || answers[0].Selected[0] != "A" {
		t.Fatalf("单选应保留选中的一项: %v", answers[0])
	}
	if len(answers[1].Selected) != 2 {
		t.Fatalf("多选应保留全部: %v", answers[1])
	}
	if answers[2].Custom != "自定义答案" || len(answers[2].Selected) != 0 {
		t.Fatalf("纯自定义答案丢失: %v", answers[2])
	}
}

func TestCommandsRequirePairingAuthorization(t *testing.T) {
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, "")
	worker.record.Mode = "pairing"
	msg := &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1", Content: "/new"}
	if err := worker.handleMessage(context.Background(), feishuMessage(msg), channelScopeKey(feishuMessage(msg))); err != nil {
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
