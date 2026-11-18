package sessions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionInputReplayPreservesHandoffErrors(t *testing.T) {
	transport := errors.New("交接中断")
	for _, test := range []struct {
		eventType string
		err       error
	}{
		{"user.message", db.ErrSessionInputConflict},
		{"user.tool_confirmation", transport},
	} {
		changes := db.SessionEventChanges{Replayed: true, Events: []db.SessionEvent{{EventType: test.eventType}}}
		calls := 0
		err := queueSessionInputChanges(t.Context(), db.Session{}, changes, func(context.Context, db.Session, []db.SessionEvent) error {
			calls++
			return test.err
		})
		if !errors.Is(err, test.err) || calls != 1 {
			t.Fatalf("不能吞掉未确认的交接失败: err=%v calls=%d", err, calls)
		}
	}
}

func TestSessionInputReplayContinuesAfterSettledControls(t *testing.T) {
	changes := db.SessionEventChanges{Replayed: true, Events: []db.SessionEvent{
		{ExternalID: "confirmation", EventType: "user.tool_confirmation"},
		{ExternalID: "custom", EventType: "user.custom_tool_result"},
		{ExternalID: "result", EventType: "user.tool_result"},
		{ExternalID: "next", EventType: "user.message"},
	}}
	var ids []string
	err := queueSessionInputChanges(t.Context(), db.Session{}, changes, func(_ context.Context, _ db.Session, events []db.SessionEvent) error {
		if len(events) != 1 {
			t.Fatalf("未知交接窗口应逐个核对原事件: %+v", events)
		}
		ids = append(ids, events[0].ExternalID)
		if events[0].EventType == "user.tool_confirmation" {
			return db.ErrSessionToolUseNotPending
		}
		if events[0].EventType != "user.message" {
			return db.ErrSessionInputConflict
		}
		return nil
	})
	if err != nil || strings.Join(ids, ",") != "confirmation,custom,result,next" {
		t.Fatalf("已解决的旧交互不能阻断后续待交接消息: %v %v", ids, err)
	}
}

func TestSessionInputInitialHandoffPreservesBatch(t *testing.T) {
	changes := db.SessionEventChanges{Events: []db.SessionEvent{{ExternalID: "first"}, {ExternalID: "second"}}}
	calls := 0
	err := queueSessionInputChanges(t.Context(), db.Session{}, changes, func(_ context.Context, _ db.Session, events []db.SessionEvent) error {
		calls++
		if len(events) != 2 || events[0].ExternalID != "first" || events[1].ExternalID != "second" {
			t.Fatalf("首次交接改变了原顺序: %+v", events)
		}
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("首次交接不应拆散原批次: %v %d", err, calls)
	}
}

func TestSessionInputRequestRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct{ key, events string }{
		{strings.Repeat("k", 129), `[]`},
		{"request", `[{`},
		{"request", `[{"type":"user.message","type":"other"}]`},
	} {
		if _, err := sessionInputRequest(test.key, []byte(test.events)); err == nil {
			t.Fatalf("非法幂等请求被接受: %q %q", test.key, test.events)
		}
	}
}

func TestSessionInputRequestCanonicalization(t *testing.T) {
	first, err := sessionInputRequest(" request ", []byte(`[{"type":"user.message","metadata":{"number":9007199254740993},"content":"原文"}]`))
	if err != nil {
		t.Fatal(err)
	}
	same, err := sessionInputRequest("request", []byte("[ { \"content\":\"原文\",\"metadata\":{\"number\":9007199254740993},\"type\":\"user.message\" } ]"))
	if err != nil || first.Key != "request" || first.Fingerprint != same.Fingerprint {
		t.Fatalf("JSON键序和信封空白不应改变请求: %+v %+v %v", first, same, err)
	}
	for _, different := range []string{
		`[{"type":"user.message","metadata":{"number":9007199254740992},"content":"原文"}]`,
		`[{"type":"user.message","metadata":{"number":9007199254740993},"content":" 原文"}]`,
	} {
		next, err := sessionInputRequest("request", []byte(different))
		if err != nil || next.Fingerprint == first.Fingerprint {
			t.Fatalf("不同输入不能共用指纹: %+v %v", next, err)
		}
	}
	if request, err := sessionInputRequest("", nil); err != nil || request != nil {
		t.Fatalf("未传幂等标识应保留原行为: %+v %v", request, err)
	}
}
