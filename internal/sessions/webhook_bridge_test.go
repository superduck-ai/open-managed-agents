package sessions

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

func TestWebhookBridgeSkipsDatabaseWithoutNotification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		h      Handler
		events []db.SessionEvent
	}{
		{name: "no enqueuer", events: []db.SessionEvent{{EventType: "session.status_running"}}},
		{name: "no events", h: Handler{webhooks: &webhooks.Enqueuer{}}},
		{name: "unmapped events", h: Handler{webhooks: &webhooks.Enqueuer{}}, events: []db.SessionEvent{
			{EventType: "agent.message"}, {EventType: "agent.thinking"}, {EventType: "span.model_request_start"},
		}},
		{name: "thread identity missing", h: Handler{webhooks: &webhooks.Enqueuer{}}, events: []db.SessionEvent{
			{EventType: "session.thread_created"}, {EventType: "session.thread_status_idle"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.h.enqueueWebhooksForSessionEvents(t.Context(), "workspace", "session", tc.events)
		})
	}
}
