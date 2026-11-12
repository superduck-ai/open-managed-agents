package sessions

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
)

type delayedPublicationBus struct {
	recordingEventBus
	mu      sync.Mutex
	entered chan struct{}
	release chan struct{}
}

func (b *delayedPublicationBus) Publish(ctx context.Context, sessionID string, envelope sessionfanout.Envelope) error {
	var payload sessionEventsFanout
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return err
	}
	if len(payload.Events) == 1 && payload.Events[0].ExternalID == "parent-start" {
		close(b.entered)
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.recordingEventBus.Publish(ctx, sessionID, envelope)
}

func TestCommittedSessionStreamCannotOvertakeEarlierFanout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	bus := &delayedPublicationBus{entered: make(chan struct{}), release: make(chan struct{})}
	handler := &Handler{eventBus: bus, eventPayloads: eventpayload.New(nil, nil), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	code := db.CodeSession{WorkspaceUUID: "workspace", SessionExternalID: "session"}
	at := time.Now().UTC()
	parent := db.SessionEvent{ExternalID: "parent-start", WorkspaceUUID: code.WorkspaceUUID, SessionExternalID: code.SessionExternalID, EventType: "span.model_request_start", ProcessedAt: at, Payload: []byte(`{"id":"parent-start","type":"span.model_request_start"}`)}
	child := db.SessionEvent{ExternalID: "child-idle", WorkspaceUUID: code.WorkspaceUUID, SessionExternalID: code.SessionExternalID, EventType: "session.thread_status_idle", ProcessedAt: at.Add(time.Millisecond), Payload: []byte(`{"id":"child-idle","type":"session.thread_status_idle"}`)}
	parentDone := make(chan error, 1)
	childDone := make(chan error, 1)
	go func() {
		parentDone <- handler.PublishCommittedCodeSessionEvents(ctx, code, db.SessionEventChanges{Events: []db.SessionEvent{parent}})
	}()
	select {
	case <-bus.entered:
	case <-ctx.Done():
		t.Fatal("first committed publication did not enter fanout")
	}
	go func() {
		childDone <- handler.PublishCommittedCodeSessionEvents(ctx, code, db.SessionEventChanges{Events: []db.SessionEvent{child}})
	}()
	childFinished := false
	select {
	case err := <-childDone:
		if err != nil {
			t.Fatal(err)
		}
		childFinished = true
	case <-time.After(100 * time.Millisecond):
	}
	close(bus.release)
	if err := <-parentDone; err != nil {
		t.Fatal(err)
	}
	if !childFinished {
		if err := <-childDone; err != nil {
			t.Fatal(err)
		}
	}
	thread := "primary"
	parent.ThreadExternalID, child.ThreadExternalID = &thread, &thread
	connection := newStreamConnection(thread, true, nil)
	committed := []sessionStreamEvent{sessionStreamEventFrom(parent), sessionStreamEventFrom(child)}
	ids := []string{}
	for _, publication := range bus.publications {
		var payload sessionEventsFanout
		if err := json.Unmarshal(publication.envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		for _, event := range payload.Events {
			emitted, _ := connection.orderedDelivery(committed, sessionEventDelivery{event: event})
			committed = nil
			for _, actual := range emitted {
				ids = append(ids, actual.ExternalID)
				var expected db.SessionEvent
				if actual.ExternalID == parent.ExternalID {
					expected = parent
				} else {
					expected = child
				}
				if string(actual.Payload) != string(expected.Payload) || !actual.ProcessedAt.Equal(expected.ProcessedAt) {
					t.Fatal("SSE changed committed payload or timestamp", actual.ExternalID)
				}
			}
		}
	}
	if !reflect.DeepEqual(ids, []string{"parent-start", "child-idle"}) {
		t.Fatalf("committed SSE was overtaken after payload restoration: %v", ids)
	}
}

func TestCommittedStreamPreservesQueuedACKAndRejectsForeignOrRepeatedFinal(t *testing.T) {
	thread, foreign := "primary", "other"
	c := newStreamConnection(thread, true, map[string]struct{}{"agent.message": {}})
	at := time.Now().UTC()
	queued := sessionStreamEvent{ExternalID: "input", EventType: "user.message", ThreadExternalID: &thread, Payload: []byte(`{"id":"input","type":"user.message","processed_at":null}`)}
	events, _ := c.orderedDelivery(nil, sessionEventDelivery{event: queued})
	if len(events) != 1 || string(events[0].Payload) != string(queued.Payload) {
		t.Fatal("queued ACK changed or dropped")
	}
	processed := queued
	processed.ProcessedAt = at
	processed.Payload = []byte(`{"id":"input","type":"user.message","processed_at":"2026-11-12T00:00:00Z"}`)
	events, _ = c.orderedDelivery([]sessionStreamEvent{processed}, sessionEventDelivery{event: queued})
	if len(events) != 1 || string(events[0].Payload) != string(processed.Payload) {
		t.Fatal("processed ACK was replaced by stale queued ACK")
	}
	events, _ = c.orderedDelivery(nil, sessionEventDelivery{event: processed})
	if len(events) != 0 {
		t.Fatal("duplicate processed notification published")
	}
	other := processed
	other.ExternalID = "foreign"
	other.ThreadExternalID = &foreign
	events, _ = c.orderedDelivery([]sessionStreamEvent{other}, sessionEventDelivery{event: other})
	if len(events) != 0 {
		t.Fatal("foreign committed event published")
	}
	start := sessionStreamEvent{ExternalID: "answer", EventType: "event_start", ThreadExternalID: &thread, Payload: []byte(`{"event":{"id":"answer","type":"agent.message"}}`)}
	if events, _ = c.orderedDelivery(nil, sessionEventDelivery{event: start}); len(events) != 1 {
		t.Fatal("valid preview start lost")
	}
	final := sessionStreamEvent{ExternalID: "answer", EventType: "agent.message", ThreadExternalID: &thread, ProcessedAt: at, Payload: []byte(`{"id":"answer","type":"agent.message","content":[{"type":"text","text":"exact"}]}`)}
	if events, _ = c.orderedDelivery([]sessionStreamEvent{final}, sessionEventDelivery{event: final}); len(events) != 1 {
		t.Fatal("final must publish exactly once")
	}
	delta := sessionStreamEvent{EventType: "event_delta", ThreadExternalID: &thread, Payload: []byte(`{"event_id":"answer","delta":{"text":"late"}}`)}
	if events, _ = c.orderedDelivery(nil, sessionEventDelivery{event: delta}); len(events) != 0 {
		t.Fatal("delta after public final published")
	}
}

func TestPublicProcessingClockPreservesUnknownNumericPayload(t *testing.T) {
	raw := json.RawMessage(`{"id":"id","type":"agent.tool_use","processed_at":"2000-01-01T00:00:00Z","input":{"integer":9007199254740993,"decimal":1.00000000000000001}}`)
	at := time.Date(2026, 11, 12, 0, 0, 0, 123000, time.UTC)
	got := eventPayloadForResponse(raw, at, "primary")
	var fields struct {
		Input       json.RawMessage `json:"input"`
		ProcessedAt time.Time       `json:"processed_at"`
	}
	if err := json.Unmarshal(got, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields.Input) != `{"integer":9007199254740993,"decimal":1.00000000000000001}` || !fields.ProcessedAt.Equal(at) {
		t.Fatalf("public clock rewrite changed unknown payload or processing time: %s", got)
	}
	if string(raw) != `{"id":"id","type":"agent.tool_use","processed_at":"2000-01-01T00:00:00Z","input":{"integer":9007199254740993,"decimal":1.00000000000000001}}` {
		t.Fatal("caller payload mutated")
	}
}
