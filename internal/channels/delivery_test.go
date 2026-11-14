package channels

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestRestartRestoresOriginalCardFromOMAHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("恢复不应重新提交输入: %s", r.Method)
			http.Error(w, "禁止写入", 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"input_1","type":"user.message"},{"id":"tool","type":"agent.tool_use","name":"bash","input":{"command":"printf restored_tool"}},{"id":"result","type":"agent.tool_result","tool_use_id":"tool","content":"restored_tool"},{"id":"answer","type":"agent.message","content":"重启后的原始答案"},{"id":"done","type":"session.status_idle","stop_reason":"end_turn"}]}`))
	}))
	defer server.Close()
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	before, _ := testWorker(t, store, server.URL)
	before.record.IncludeToolCalls = true
	before.record.IncludeThinking = true
	first := &independentAdapter{worker: before}
	before.adapter = first
	task := &channelReplyTask{inputEventID: "input_1", scopeKey: "chat:oc_1", sessionID: "sesn_1"}
	msg := &channelMessage{MessageID: "message_1", ChatID: "oc_1", ChatType: "p2p", UserID: "actor"}
	originalStream, err := before.openReply(t.Context(), task, msg)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	row := store.deliveries["chnl_1:input_1"]
	row.LeaseUntil = time.Now().Add(-time.Second)
	store.deliveries["chnl_1:input_1"] = row
	store.mu.Unlock()
	after, _ := testWorker(t, store, server.URL)
	after.supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent {
		return []omaEvent{{ID: "input_1", Type: "user.message"}, {ID: "tool", Type: "agent.tool_use", Name: "bash", Input: []byte(`{"command":"printf restored_tool"}`)}, {ID: "result", Type: "agent.tool_result", ToolUseID: "tool", Content: []byte(`"restored_tool"`)}, {ID: "answer", Type: "agent.message", Content: []byte(`"重启后的原始答案"`)}, {ID: "end", Type: "session.status_idle", StopReason: []byte(`"end_turn"`)}}
	})
	second := &independentAdapter{worker: after}
	after.adapter = second
	after.online.Store(true)
	after.restoreReplies(t.Context())
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		done := store.deliveries["chnl_1:input_1"].State == "completed"
		store.mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := originalStream.update(t.Context(), &channelReply{}); !errors.Is(err, db.ErrInvalidState) {
		t.Fatalf("失权旧实例不得再更新: %v", err)
	}
	second.mu.Lock()
	defer second.mu.Unlock()
	if !strings.Contains(strings.Join(second.toolDetails, ""), "restored_tool") {
		t.Fatalf("恢复丢失原工具面板: %v", second.toolDetails)
	}
	if first.opens != 1 || second.opens != 0 || len(second.resumed) != 1 || second.resumed[0] != "independent_card" || !strings.Contains(strings.Join(second.replies, ""), "重启后的原始答案") {
		t.Fatalf("应只续写原卡: before=%d after=%d receipts=%v replies=%v", first.opens, second.opens, second.resumed, second.replies)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.deliveries["chnl_1:input_1"].State != "completed" {
		t.Fatal("恢复后没有确认交付")
	}
}

func TestUnknownCardReceiptIsNotSentAgain(t *testing.T) {
	store := &fakeRuntimeStore{}
	worker, _ := testWorker(t, store, "http://unused")
	adapter := &independentAdapter{worker: worker}
	worker.adapter = adapter
	store.deliveries = map[string]db.ChannelReplyDelivery{"chnl_1:input": {ChannelExternalID: "chnl_1", InputEventID: "input", State: "sending", LeaseUntil: time.Now().Add(-time.Hour)}}
	_, err := worker.openReply(t.Context(), &channelReplyTask{inputEventID: "input", sessionID: "sesn_1"}, &channelMessage{ChatID: "oc_1", ChatType: "p2p", UserID: "actor"})
	if err == nil || adapter.opens != 0 {
		t.Fatalf("未知回执不能新发卡片: err=%v opens=%d", err, adapter.opens)
	}
}

func TestReplyRecoveryDoesNotCrossChannelAccounts(t *testing.T) {
	for _, change := range []string{"类型", "账号"} {
		t.Run(change, func(t *testing.T) {
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
			worker, _ := testWorker(t, store, "http://unused")
			adapter := &independentAdapter{worker: worker}
			worker.adapter = adapter
			worker.online.Store(true)
			row := db.ChannelReplyDelivery{ChannelExternalID: "chnl_1", ChannelType: worker.record.ChannelType, AccountID: worker.record.AppID, InputEventID: "input", SessionExternalID: "sesn_1", ScopeKey: "chat:oc_1", State: "sent", ReplyMessageID: "old_card", LeaseUntil: time.Now().Add(-time.Hour)}
			if change == "类型" {
				row.ChannelType = "other"
			} else {
				row.AccountID = "other"
			}
			store.deliveries = map[string]db.ChannelReplyDelivery{"chnl_1:input": row}
			worker.restoreReplies(t.Context())
			worker.mu.Lock()
			pending := len(worker.replies)
			worker.mu.Unlock()
			if pending != 0 || len(adapter.resumed) != 0 || adapter.opens != 0 {
				t.Fatal("旧账号卡片不应被当前账号接管")
			}
		})
	}
}

func TestShutdownReleasesOriginalCardLease(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "停止任务仍持有原卡", true: "停机释放原卡"}[shutdown], func(t *testing.T) {
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
			worker, _ := testWorker(t, store, "http://unused")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			worker.ctx = ctx
			task := worker.startReplyTask(ctx, "chat:oc_1", "sesn_1")
			defer worker.finishReply(task)
			task.inputEventID = "shutdown_input"
			stream, err := worker.openReply(ctx, task, &channelMessage{ChatID: "oc_1", ChatType: "p2p", UserID: "actor"})
			if err != nil {
				t.Fatal(err)
			}
			if shutdown {
				cancel()
			} else {
				task.cancel()
			}
			if err := stream.close(t.Context()); err != nil {
				t.Fatal(err)
			}
			row, claimed, err := store.ClaimChannelReplyDelivery(t.Context(), worker.deliveryScope(), task.inputEventID, "new_instance")
			if err != nil || claimed != shutdown {
				t.Fatalf("停机释放与任务停止必须区分: shutdown=%v claimed=%v err=%v", shutdown, claimed, err)
			}
			if shutdown && (row.State != "sent" || row.ReplyMessageID != stream.receipt()) {
				t.Fatalf("接管必须保留原卡回执: %+v", row)
			}
		})
	}
}

func TestRecoveryStartsAtLeaseExpiryWithoutReconcileDelay(t *testing.T) {
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, "http://unused")
	adapter := &independentAdapter{worker: worker, started: make(chan struct{}), stopped: make(chan struct{})}
	worker.adapter = adapter
	store.deliveries = map[string]db.ChannelReplyDelivery{"chnl_1:input_expiry": {ChannelExternalID: "chnl_1", ChannelType: "feishu", AccountID: "app_1", InputEventID: "input_expiry", SessionExternalID: "sesn_1", ScopeKey: "chat:oc_1", MessageID: "original_message", ChatID: "oc_1", ChatType: "p2p", ActorID: "actor", ReplyMessageID: "original_card", State: "sent", LeaseOwner: "crashed_instance", LeaseUntil: time.Now().Add(1500 * time.Millisecond)}}
	worker.supervisor.subscribeEvents = testEventSubscriber(t, func() []omaEvent {
		return []omaEvent{{ID: "input_expiry", Type: "user.message"}, {ID: "answer", Type: "agent.message", Content: []byte(`"租约到期后恢复"`)}, {ID: "done", Type: "session.status_idle", StopReason: []byte(`"end_turn"`)}}
	})
	worker.start()
	defer worker.close()
	<-adapter.started
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		store.mu.Lock()
		row := store.deliveries["chnl_1:input_expiry"]
		store.mu.Unlock()
		if row.State == "completed" {
			adapter.mu.Lock()
			defer adapter.mu.Unlock()
			if adapter.opens != 0 || len(adapter.resumed) != 1 || adapter.resumed[0] != "original_card" {
				t.Fatalf("到期接管必须续写原卡: opens=%d resumed=%v", adapter.opens, adapter.resumed)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("租约已到期仍等待30秒全局扫描")
		case <-ticker.C:
		}
	}
}
