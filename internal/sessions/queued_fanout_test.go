package sessions

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestQueuedUserMessagePublishesNullThenProcessedSSE(t *testing.T) {
	bus := &recordingEventBus{}
	h := &Handler{eventBus: bus, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), streams: newStreamHub()}
	thread := "thread"
	pending := db.SessionEvent{ExternalID: "queued", WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: &thread, EventType: "user.message", Payload: json.RawMessage(`{"id":"queued","type":"user.message","content":[{"type":"text","text":"B"}],"processed_at":null}`)}
	h.publishSessionEvents(context.Background(), []db.SessionEvent{pending})
	processed := pending
	processed.ProcessedAt = time.Date(2026, 10, 17, 5, 10, 0, 123456000, time.UTC)
	h.publishSessionEvents(context.Background(), []db.SessionEvent{processed})
	if len(bus.publications) != 2 {
		t.Fatalf("fanout publications=%d, want pending and processed versions", len(bus.publications))
	}
	sub, ch := h.streams.subscribe("workspace", "session")
	defer h.streams.unsubscribe(sub)
	c := newStreamConnection(thread, true, nil)
	recorder := httptest.NewRecorder()
	for _, publication := range bus.publications {
		var payload sessionEventsFanout
		if err := json.Unmarshal(publication.envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		h.receiveSessionEventsFanout(payload.Events)
		select {
		case delivery := <-ch:
			event, accepted := c.event(delivery)
			if !accepted {
				t.Fatal("same ID lifecycle version rejected")
			}
			writeSSE(recorder, event, thread)
		default:
			t.Fatal("fanout did not reach subscribed SSE connection")
		}
	}
	body := recorder.Body.String()
	if strings.Count(body, "id: queued\n") != 2 {
		t.Fatalf("sameID versions not retained: %s", body)
	}
	var versions []map[string]any
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, payload)
	}
	if len(versions) != 2 {
		t.Fatalf("versions=%d", len(versions))
	}
	if value, ok := versions[0]["processed_at"]; !ok || value != nil {
		t.Fatalf("queued processed_at=%v present=%t", value, ok)
	}
	if versions[1]["processed_at"] != formatEventTime(processed.ProcessedAt) {
		t.Fatalf("processed version=%v", versions[1])
	}
	for _, v := range versions {
		if v["id"] != "queued" || v["type"] != "user.message" || v["session_thread_id"] != thread {
			t.Fatalf("identity/routing changed: %v", v)
		}
	}
}
