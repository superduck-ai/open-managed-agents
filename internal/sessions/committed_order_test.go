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
	parent := db.SessionEvent{ExternalID: "parent-start", WorkspaceUUID: code.WorkspaceUUID, SessionExternalID: code.SessionExternalID, EventType: "span.model_request_start", ProcessedAt: at, Seq: 1, Payload: []byte(`{"id":"parent-start","type":"span.model_request_start"}`)}
	child := db.SessionEvent{ExternalID: "child-idle", WorkspaceUUID: code.WorkspaceUUID, SessionExternalID: code.SessionExternalID, EventType: "session.thread_status_idle", ProcessedAt: at.Add(time.Millisecond), Seq: 2, Payload: []byte(`{"id":"child-idle","type":"session.thread_status_idle"}`)}
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
	order := fakeCommittedOrder(0, sessionStreamEventFrom(parent), sessionStreamEventFrom(child))
	ids := []string{}
	for _, publication := range bus.publications {
		var payload sessionEventsFanout
		if err := json.Unmarshal(publication.envelope.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		for _, event := range payload.Events {
			emitted, _, err := order.events(t.Context(), connection, sessionEventDelivery{event: event})
			if err != nil {
				t.Fatal(err)
			}
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

func fakeCommittedOrder(last int64, committed ...sessionStreamEvent) *committedStreamOrder {
	return &committedStreamOrder{
		last: last,
		boundary: func(context.Context) (int64, error) {
			if len(committed) == 0 {
				return 0, nil
			}
			return committed[len(committed)-1].Seq, nil
		},
		history: func(_ context.Context, after, through int64) ([]sessionStreamEvent, error) {
			var result []sessionStreamEvent
			for _, event := range committed {
				if event.Seq > after && event.Seq <= through {
					result = append(result, event)
				}
			}
			return result, nil
		},
	}
}

func deliver(t *testing.T, order *committedStreamOrder, c *streamConnection, delivery streamDelivery) []sessionStreamEvent {
	t.Helper()
	events, _, err := order.events(t.Context(), c, delivery)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestCommittedStreamRejectsStaleOrForeignEvents(t *testing.T) {
	thread, foreign := "primary", "other"
	at := time.Now().UTC()
	queued := sessionStreamEvent{ExternalID: "input", EventType: "user.message", ThreadExternalID: &thread, Payload: []byte(`{"id":"input","type":"user.message","processed_at":null}`)}
	processed := queued
	processed.ProcessedAt, processed.Seq = at, 1
	processed.Payload = []byte(`{"id":"input","type":"user.message","processed_at":"2026-11-12T00:00:00Z"}`)
	other := sessionStreamEvent{ExternalID: "foreign", EventType: "agent.message", ThreadExternalID: &foreign, ProcessedAt: at, Seq: 2, Payload: []byte(`{"id":"foreign","type":"agent.message"}`)}
	c := newStreamConnection(thread, true, map[string]struct{}{"agent.message": {}})
	order := fakeCommittedOrder(0, processed, other)

	if events := deliver(t, order, c, sessionEventDelivery{event: other}); len(events) != 1 || events[0].ExternalID != "input" {
		t.Fatalf("gap fill must deliver the earlier committed event only once and drop the foreign one: %+v", events)
	}
	if events := deliver(t, order, c, sessionEventDelivery{event: queued}); len(events) != 0 {
		t.Fatal("stale queued ACK replaced the processed input")
	}
	if events := deliver(t, order, c, sessionEventDelivery{event: processed}); len(events) != 0 {
		t.Fatal("duplicate processed notification published")
	}
	if order.last != 2 {
		t.Fatalf("order position=%d", order.last)
	}
}

func TestCommittedStreamCatchesUpLostNotificationsAndDeletion(t *testing.T) {
	thread, foreign := "primary", "other"
	at := time.Now().UTC()
	lost := sessionStreamEvent{ExternalID: "lost", EventType: "agent.message", ThreadExternalID: &thread, ProcessedAt: at, Seq: 1, Payload: []byte(`{"id":"lost","type":"agent.message"}`)}
	deleted := sessionStreamEvent{ExternalID: "deleted", EventType: "session.deleted", ThreadExternalID: &foreign, ProcessedAt: at, Seq: 2, Payload: []byte(`{"id":"deleted","type":"session.deleted"}`)}
	c := newStreamConnection(thread, true, nil)
	order := fakeCommittedOrder(0, lost, deleted)
	events, closed, err := order.events(t.Context(), c, nil)
	if err != nil || !closed || len(events) != 1 || events[0].ExternalID != "lost" {
		t.Fatalf("keepalive catch-up events=%+v closed=%v err=%v", events, closed, err)
	}
	if _, closed, _ := order.events(t.Context(), c, sessionEventDelivery{event: deleted}); !closed {
		t.Fatal("late deletion notification did not close the stream")
	}
}

func TestCommittedStreamDeliversInOrderAndPreviewsWithoutReads(t *testing.T) {
	thread := "primary"
	at := time.Now().UTC()
	c := newStreamConnection(thread, true, map[string]struct{}{"agent.message": {}})
	order := fakeCommittedOrder(4)
	order.history = func(context.Context, int64, int64) ([]sessionStreamEvent, error) {
		t.Fatal("in-order delivery read history")
		return nil, nil
	}
	start := sessionStreamEvent{ExternalID: "answer", EventType: "event_start", ThreadExternalID: &thread, Payload: []byte(`{"event":{"id":"answer","type":"agent.message"}}`)}
	if events := deliver(t, order, c, sessionEventDelivery{event: start}); len(events) != 1 {
		t.Fatal("valid preview start lost")
	}
	final := sessionStreamEvent{ExternalID: "answer", EventType: "agent.message", ThreadExternalID: &thread, ProcessedAt: at, Seq: 5, Payload: []byte(`{"id":"answer","type":"agent.message","content":[{"type":"text","text":"exact"}]}`)}
	if events := deliver(t, order, c, sessionEventDelivery{event: final}); len(events) != 1 || order.last != 5 {
		t.Fatal("next committed event must publish exactly once")
	}
	delta := sessionStreamEvent{EventType: "event_delta", ThreadExternalID: &thread, Payload: []byte(`{"event_id":"answer","delta":{"text":"late"}}`)}
	if events := deliver(t, order, c, sessionEventDelivery{event: delta}); len(events) != 0 {
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
