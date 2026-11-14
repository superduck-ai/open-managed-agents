package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
)

func subscriptionFixture(t *testing.T) (*Handler, *EventSubscription) {
	t.Helper()
	h := &Handler{streams: newStreamHub(), eventBus: sessionfanout.NewLocal()}
	sub, err := h.subscribeSession(t.Context(), db.Session{WorkspaceUUID: "workspace", ExternalID: "session"}, newStreamConnection("primary", true, map[string]struct{}{"agent.message": {}}), StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sub.Close)
	return h, sub
}

func TestInternalSubscriptionRequiresPrincipal(t *testing.T) {
	if _, err := new(Handler).SubscribeEvents(t.Context(), "session", StreamOptions{}); err == nil {
		t.Fatal("内部订阅不能绕过运行身份")
	}
}

func TestInternalSubscriptionCloseStopsReplay(t *testing.T) {
	_, sub := subscriptionFixture(t)
	sub.replay = []sessionStreamEvent{{EventType: "agent.message", ExternalID: "queued", PrimaryThread: true}}
	sub.Close()
	if _, err := sub.Receive(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后不能继续交付历史: %v", err)
	}
}

func TestInternalSubscriptionPreservesTenantAndThreadFiltering(t *testing.T) {
	h, sub := subscriptionFixture(t)
	for _, event := range []sessionStreamEvent{
		{WorkspaceUUID: "other", SessionExternalID: "session", PrimaryThread: true, ExternalID: "wrong_workspace", EventType: "agent.message"},
		{WorkspaceUUID: "workspace", SessionExternalID: "other", PrimaryThread: true, ExternalID: "wrong_session", EventType: "agent.message"},
		{WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: new("child"), ExternalID: "wrong_thread", EventType: "agent.message"},
		{WorkspaceUUID: "workspace", SessionExternalID: "session", PrimaryThread: true, ExternalID: "correct", EventType: "agent.message"},
	} {
		h.streams.broadcastEvent(event)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	event, err := sub.Receive(ctx)
	if err != nil || event.ExternalID != "correct" {
		t.Fatalf("租户/会话/线程过滤失败: %+v %v", event, err)
	}
}

func TestInternalSubscriptionReplaySuppressesQueuedDuplicates(t *testing.T) {
	h, sub := subscriptionFixture(t)
	event := sessionStreamEvent{WorkspaceUUID: "workspace", SessionExternalID: "session", PrimaryThread: true, ExternalID: "history", EventType: "agent.message"}
	sub.cursorID = "cursor"
	sub.replay = []sessionStreamEvent{event}
	sub.replayHistory = sub.replay
	h.streams.broadcastEvent(event)
	live := event
	live.ExternalID = "live"
	h.streams.broadcastEvent(live)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, want := range []string{"history", "live"} {
		got, err := sub.Receive(ctx)
		if err != nil || got.ExternalID != want {
			t.Fatalf("补读与实时交界重复或遗漏: got=%+v want=%s err=%v", got, want, err)
		}
	}
}

func TestInternalSubscriptionSlowConsumerDoesNotBlockOthers(t *testing.T) {
	h, slow := subscriptionFixture(t)
	fast, err := h.subscribeSession(t.Context(), db.Session{WorkspaceUUID: "workspace", ExternalID: "session"}, newStreamConnection("primary", true, nil), StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	for i := range 1024 {
		h.streams.broadcastEvent(sessionStreamEvent{WorkspaceUUID: "workspace", SessionExternalID: "session", PrimaryThread: true, ExternalID: fmt.Sprintf("event_%d", i), EventType: "agent.message"})
		if _, err := fast.Receive(ctx); err != nil {
			t.Fatalf("慢订阅影响了正常订阅: %v", err)
		}
	}
	for {
		_, err := slow.Receive(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("慢订阅未按已有策略断开: %v", err)
		}
	}
	t.Logf("阻塞一个消费者时正常订阅交付1024条，耗时%s", time.Since(started))
}

func TestInternalSubscriptionSignalsGapAndReleasesOnCancel(t *testing.T) {
	h, sub := subscriptionFixture(t)
	h.streams.resetSession("session")
	if _, err := sub.Receive(t.Context()); !errors.Is(err, ErrStreamReset) {
		t.Fatalf("断线必须触发补读: %v", err)
	}
	sub.Close()
	sub.Close()
	ctx, cancel := context.WithCancel(t.Context())
	next, err := h.subscribeSession(ctx, db.Session{WorkspaceUUID: "workspace", ExternalID: "session"}, newStreamConnection("primary", true, nil), StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, open := <-next.deliveries:
		if open {
			t.Fatal("取消后订阅未释放")
		}
	case <-time.After(time.Second):
		t.Fatal("取消后订阅仍然存活")
	}
}

func TestInternalSubscriptionPreservesChinesePreviewAndFinal(t *testing.T) {
	h, sub := subscriptionFixture(t)
	payloads := []string{
		`{"type":"event_start","event":{"id":"preview","type":"agent.message"}}`,
		`{"type":"event_delta","event_id":"preview","delta":{"type":"content_delta","index":0,"content":{"type":"text","text":"中文🧑‍💻"}}}`,
		`{"id":"preview","type":"agent.message","content":"中文🧑‍💻"}`,
	}
	for i, kind := range []string{"event_start", "event_delta", "agent.message"} {
		h.streams.broadcastEvent(sessionStreamEvent{WorkspaceUUID: "workspace", SessionExternalID: "session", PrimaryThread: true, ExternalID: "preview", EventType: kind, Payload: json.RawMessage(payloads[i])})
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, want := range payloads {
		event, err := sub.Receive(ctx)
		if err != nil || string(event.Payload) != want {
			t.Fatalf("中文增量顺序或正文改变: %+v %v", event, err)
		}
	}
}

func TestInternalSubscriptionReplayAllowsProcessedInputUpdate(t *testing.T) {
	for _, kind := range []string{"user.message", "user.tool_confirmation"} {
		t.Run(kind, func(t *testing.T) {
			h, sub := subscriptionFixture(t)
			pending := sessionStreamEvent{WorkspaceUUID: "workspace", SessionExternalID: "session", PrimaryThread: true, ExternalID: "input", EventType: kind}
			sub.cursorID = "input"
			sub.replay = []sessionStreamEvent{pending}
			sub.replayHistory = sub.replay
			h.streams.broadcastEvent(pending)
			processed := pending
			processed.ProcessedAt = time.Now()
			h.streams.broadcastEvent(processed)
			h.streams.broadcastEvent(processed)
			next := processed
			next.ExternalID = "next"
			next.EventType = "agent.message"
			h.streams.broadcastEvent(next)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			for index, want := range []sessionStreamEvent{pending, processed, next} {
				got, err := sub.Receive(ctx)
				if err != nil || got.ExternalID != want.ExternalID || !got.ProcessedAt.Equal(want.ProcessedAt) {
					t.Fatalf("第%d个事件丢失或重复: got=%+v want=%+v err=%v", index, got, want, err)
				}
			}
		})
	}
}

func TestInternalSubscriptionWaitsForInputBeforeOldRound(t *testing.T) {
	h, sub := subscriptionFixture(t)
	sub.cursorID = "pending_input"
	sub.waitingInput = true
	for _, event := range []sessionStreamEvent{
		{ExternalID: "previous_answer", EventType: "agent.message", ProcessedAt: time.Now()},
		{ExternalID: "previous_idle", EventType: "session.status_idle", ProcessedAt: time.Now()},
		{ExternalID: "pending_input", EventType: "user.message"},
	} {
		event.WorkspaceUUID, event.SessionExternalID, event.PrimaryThread = "workspace", "session", true
		h.streams.broadcastEvent(event)
	}
	h.streams.resetSession("session")
	if _, err := sub.Receive(t.Context()); !errors.Is(err, ErrStreamReset) {
		t.Fatalf("输入未处理时交付了上一轮或待处理消息: %v", err)
	}
}

func TestInternalSubscriptionDropsLiveEventsBeforeInputBoundary(t *testing.T) {
	h, sub := subscriptionFixture(t)
	sub.inputBoundary = time.Now()
	for index, kind := range []string{"agent.message", "session.status_idle", "agent.message"} {
		processed := sub.inputBoundary.Add(-time.Second)
		if index == 2 {
			processed = sub.inputBoundary.Add(time.Second)
		}
		h.streams.broadcastEvent(sessionStreamEvent{WorkspaceUUID: "workspace", SessionExternalID: "session", PrimaryThread: true, ExternalID: fmt.Sprintf("event_%d", index), EventType: kind, ProcessedAt: processed})
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	got, err := sub.Receive(ctx)
	if err != nil || got.ExternalID != "event_2" {
		t.Fatalf("实时队列中的旧轮次未过滤: %+v %v", got, err)
	}
}

func TestInternalSubscriptionServicesMaintenanceDuringReplay(t *testing.T) {
	_, sub := subscriptionFixture(t)
	sub.replay = []sessionStreamEvent{{ExternalID: "queued", EventType: "agent.message", PrimaryThread: true}}
	wake := make(chan time.Time, 1)
	wake <- time.Now()
	if _, err := sub.ReceiveUntil(t.Context(), wake); !errors.Is(err, ErrStreamHeartbeat) {
		t.Fatalf("历史补读不能饿死已到期的维护任务: %v", err)
	}
	event, err := sub.Receive(t.Context())
	if err != nil || event.ExternalID != "queued" {
		t.Fatalf("维护信号不能吞掉补读事件: %+v %v", event, err)
	}
}
