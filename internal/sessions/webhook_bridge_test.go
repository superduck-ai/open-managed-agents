package sessions

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWebhookEventsFromSessionEventAliases(t *testing.T) {
	// 现在式/过去式别名仍映射到统一 webhook 事件（回归守卫）
	cases := []struct {
		eventType string
		want      string
	}{
		{"session.status_running", "session.status_run_started"},
		{"session.running", "session.status_run_started"},
		{"session.status_idle", "session.status_idled"},
		{"session.idled", "session.status_idled"},
		{"session.requires_action", "session.status_idled"},
		{"session.thread_status_idle", "session.thread_status_idle"},
		{"session.thread_idled", "session.thread_idled"},
	}
	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			events := webhookEventsFromSessionEvent(db.SessionEvent{EventType: tc.eventType})
			if len(events) == 0 {
				t.Fatalf("webhookEventsFromSessionEvent(%s) 无映射", tc.eventType)
			}
			found := false
			for _, e := range events {
				if e.EventType == tc.want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("webhookEventsFromSessionEvent(%s) = %v, want 包含 %s", tc.eventType, events, tc.want)
			}
		})
	}
}

func TestWebhookEventsFromSessionEventUnknown(t *testing.T) {
	// 未知事件不产生 webhook 事件
	events := webhookEventsFromSessionEvent(db.SessionEvent{EventType: "session.not_a_real_event"})
	if len(events) != 0 {
		t.Fatalf("未知事件应无映射, got %v", events)
	}
}
