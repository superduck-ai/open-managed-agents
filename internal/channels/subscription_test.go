package channels

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

type testEventSubscription struct {
	events  []sessions.StreamEvent
	failure error
}

func (s *testEventSubscription) ReceiveUntil(ctx context.Context, wake <-chan time.Time) (sessions.StreamEvent, error) {
	if len(s.events) == 0 && s.failure != nil {
		return sessions.StreamEvent{}, s.failure
	}
	if len(s.events) == 0 {
		select {
		case <-ctx.Done():
			return sessions.StreamEvent{}, ctx.Err()
		case <-wake:
			return sessions.StreamEvent{}, sessions.ErrStreamHeartbeat
		}
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}
func (*testEventSubscription) Close() {}

func testEventSubscriber(t *testing.T, source func() []omaEvent) SessionEventSubscriber {
	t.Helper()
	return func(_ context.Context, _, _ string, options sessions.StreamOptions) (SessionEventSubscription, error) {
		events := source()
		if options.AfterEventID != "" {
			for index, event := range events {
				if event.ID == options.AfterEventID {
					events = events[index:]
					if !options.WaitForInput {
						events = events[1:]
					}
					break
				}
			}
		}
		sub := &testEventSubscription{}
		for _, event := range events {
			payload, err := json.Marshal(event)
			if err != nil {
				return nil, err
			}
			processed := time.Now()
			sub.events = append(sub.events, sessions.StreamEvent{ExternalID: event.ID, EventType: event.Type, Payload: payload, ProcessedAt: processed})
		}
		return sub, nil
	}
}

func TestChannelSubscriptionResumesFromLastDeliveredEvent(t *testing.T) {
	worker, card := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, "http://unused")
	calls := 0
	worker.supervisor.subscribeEvents = func(ctx context.Context, key, sessionID string, options sessions.StreamOptions) (SessionEventSubscription, error) {
		calls++
		if calls == 1 {
			if options.AfterEventID != "input" || !options.WaitForInput {
				t.Fatalf("首次订阅丢失输入锚点: %+v", options)
			}
			sub, err := testEventSubscriber(t, func() []omaEvent {
				return []omaEvent{{ID: "input", Type: "user.message"}, {ID: "first", Type: "agent.message", Content: []byte(`"中文第一段"`)}}
			})(ctx, key, sessionID, options)
			if err != nil {
				return nil, err
			}
			sub.(*testEventSubscription).events = append(sub.(*testEventSubscription).events,
				sessions.StreamEvent{ExternalID: "temporary_preview", EventType: "event_start", Payload: []byte(`{"event":{"id":"temporary_preview","type":"agent.message"}}`)},
				sessions.StreamEvent{ExternalID: "temporary_preview", EventType: "event_delta", Payload: []byte(`{"event_id":"temporary_preview","delta":{"content":{"text":"断线预览"}}}`)},
			)
			sub.(*testEventSubscription).failure = sessions.ErrStreamReset
			return sub, nil
		}
		if calls != 2 || options.AfterEventID != "first" || options.WaitForInput {
			t.Fatalf("断线必须使用已交付的持久事件: calls=%d options=%+v", calls, options)
		}
		return testEventSubscriber(t, func() []omaEvent {
			return []omaEvent{{ID: "first", Type: "agent.message", Content: []byte(`"中文第一段"`)}, {ID: "second", Type: "agent.message", Content: []byte(`"第二段🧑‍💻"`)}, {ID: "done", Type: "session.status_idle", StopReason: []byte(`"end_turn"`)}}
		})(ctx, key, sessionID, sessions.StreamOptions{})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	task := &channelReplyTask{ctx: ctx, scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "input"}
	if err := worker.streamReply(ctx, task, &channelMessage{ChatID: "oc_1", ChatType: "p2p", UserID: "actor"}, "key"); err != nil {
		t.Fatal(err)
	}
	card.mu.Lock()
	defer card.mu.Unlock()
	last := card.appends[len(card.appends)-1]
	if calls != 2 || !task.normalEnd || strings.Count(last, "中文第一段") != 1 || !strings.Contains(last, "第二段🧑‍💻") || strings.Contains(last, "断线预览") {
		t.Fatalf("补读重复或遗漏: calls=%d normal=%v card=%s", calls, task.normalEnd, last)
	}
}

func TestReplySubscriptionInheritsCallerLifecycle(t *testing.T) {
	for _, hasDeadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "不附加固定有效期", true: "保留调用方期限"}[hasDeadline], func(t *testing.T) {
			worker, _ := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, "http://unused")
			ctx := t.Context()
			if hasDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(time.Hour))
				defer cancel()
			}
			wantDeadline, wantHasDeadline := ctx.Deadline()
			source := testEventSubscriber(t, func() []omaEvent {
				return []omaEvent{{ID: "input", Type: "user.message"}, {ID: "answer", Type: "agent.message", Content: []byte(`"按会话生命周期完成"`)}, {ID: "done", Type: "session.status_idle", StopReason: []byte(`"end_turn"`)}}
			})
			worker.supervisor.subscribeEvents = func(subCtx context.Context, key, sessionID string, options sessions.StreamOptions) (SessionEventSubscription, error) {
				deadline, hasDeadline := subCtx.Deadline()
				if hasDeadline != wantHasDeadline || !deadline.Equal(wantDeadline) {
					t.Fatalf("渠道不能附加独立有效期: got=%v/%v want=%v/%v", deadline, hasDeadline, wantDeadline, wantHasDeadline)
				}
				return source(subCtx, key, sessionID, options)
			}
			task := &channelReplyTask{ctx: ctx, scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "input"}
			if err := worker.streamReply(ctx, task, &channelMessage{ChatID: "oc_1", ChatType: "p2p", UserID: "actor"}, "key"); err != nil {
				t.Fatal(err)
			}
			if !task.normalEnd {
				t.Fatal("会话结束事件应完成渠道回复")
			}
		})
	}
}
