package channels

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

type isolatedTestSubscription struct{ events <-chan sessions.StreamEvent }

func (s isolatedTestSubscription) ReceiveUntil(ctx context.Context, wake <-chan time.Time) (sessions.StreamEvent, error) {
	select {
	case <-ctx.Done():
		return sessions.StreamEvent{}, ctx.Err()
	case <-wake:
		return sessions.StreamEvent{}, sessions.ErrStreamHeartbeat
	case event := <-s.events:
		return event, nil
	}
}
func (isolatedTestSubscription) Close() {}

type blockedTestReply struct{ entered chan struct{} }

func (s blockedTestReply) receipt() string             { return "blocked_card" }
func (s blockedTestReply) close(context.Context) error { return nil }
func (s blockedTestReply) update(ctx context.Context, _ *channelReply) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

type capturedTestUpdate struct {
	card string
	at   time.Time
}
type capturedTestReply struct{ updates chan capturedTestUpdate }

func (s capturedTestReply) receipt() string             { return "fast_card" }
func (s capturedTestReply) close(context.Context) error { return nil }
func (s capturedTestReply) update(_ context.Context, reply *channelReply) error {
	s.updates <- capturedTestUpdate{card: reply.render(), at: time.Now()}
	return nil
}

func TestBlockedCardDoesNotDelayAnotherSessionPreview(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:slow": "slow_session", "chat:fast": "fast_session"}}, "http://unused")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	slowCtx, stopSlow := context.WithCancel(ctx)
	defer stopSlow()
	slow := newReplyRunner(worker, &channelReplyTask{scopeKey: "chat:slow", sessionID: "slow_session"}, &channelMessage{})
	entered := make(chan struct{})
	slow.stream = blockedTestReply{entered: entered}
	slowEvents := make(chan sessions.StreamEvent, 1)
	slowEvents <- sessions.StreamEvent{ExternalID: "slow_message", EventType: "agent.message", Payload: []byte(`{"content":"阻塞卡片"}`)}
	slowDone := make(chan error, 1)
	go func() {
		_, _, _, err := slow.receiveSubscription(slowCtx, isolatedTestSubscription{slowEvents}, "", false)
		slowDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("未进入平台更新阻塞边界")
	}
	fast := newReplyRunner(worker, &channelReplyTask{scopeKey: "chat:fast", sessionID: "fast_session"}, &channelMessage{})
	updates := make(chan capturedTestUpdate, 8)
	fast.stream = capturedTestReply{updates: updates}
	if err := fast.update(ctx); err != nil {
		t.Fatal(err)
	}
	<-updates
	fastEvents := make(chan sessions.StreamEvent, 4)
	fastDone := make(chan error, 1)
	go func() {
		_, _, _, err := fast.receiveSubscription(ctx, isolatedTestSubscription{fastEvents}, "", false)
		fastDone <- err
	}()
	started := time.Now()
	fastEvents <- sessions.StreamEvent{ExternalID: "preview", EventType: "event_start", Payload: []byte(`{"event":{"id":"preview","type":"agent.message"}}`)}
	fastEvents <- sessions.StreamEvent{ExternalID: "preview", EventType: "event_delta", Payload: []byte(`{"event_id":"preview","delta":{"content":{"text":"中文🧑‍💻首条增量"}}}`)}
	select {
	case update := <-updates:
		latency := update.at.Sub(started)
		if !strings.Contains(update.card, "中文🧑‍💻首条增量") || latency > time.Second {
			t.Fatalf("另一会话增量被阻塞或丢失: latency=%s card=%s", latency, update.card)
		}
		t.Logf("同渠道另一张卡片更新阻塞时，首条中文增量到卡片更新耗时%s", latency)
	case <-ctx.Done():
		t.Fatal("被阻塞卡片影响其他会话的首条增量")
	}
	fastEvents <- sessions.StreamEvent{ExternalID: "preview", EventType: "agent.message", Payload: []byte(`{"content":"中文🧑‍💻首条增量与最终正文"}`)}
	fastEvents <- sessions.StreamEvent{ExternalID: "done", EventType: "session.status_idle", Payload: []byte(`{"stop_reason":"end_turn"}`)}
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("其他会话未独立完成")
	}
	if got := fast.reply.render(); strings.Count(got, "中文🧑‍💻") != 1 || !strings.Contains(got, "已完成") {
		t.Fatalf("最终卡片重复或未结束: %s", got)
	}
	select {
	case <-slowDone:
		t.Fatal("阻塞故障被其他会话完成错误终止")
	default:
	}
	stopSlow()
	select {
	case err := <-slowDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("阻塞消费者取消未生效: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("阻塞更新不能响应取消")
	}
}
