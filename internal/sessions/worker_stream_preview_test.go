package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
)

func TestWorkerPreviewConverterHandlesConcurrentBatches(t *testing.T) {
	const batchCount = 32
	converter := newWorkerPreviewConverter()
	errs := make(chan error, batchCount)
	var workers sync.WaitGroup
	workers.Add(batchCount)
	for index := range batchCount {
		go func() {
			defer workers.Done()
			batch := previewTestBatch()
			batch.CodeSessionID = fmt.Sprintf("cse-%d", index)
			events := convertPreviewTestPayloads(converter, batch, []json.RawMessage{
				json.RawMessage(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_test"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"raw-start"}`),
				json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-start"}`),
			})
			if len(events) != 1 {
				errs <- fmt.Errorf("batch %d produced %d preview events, want 1", index, len(events))
			}
		}()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestWorkerPreviewConverterMessageStopClearsAllBlocks(t *testing.T) {
	converter := newWorkerPreviewConverter()
	batch := previewTestBatch()
	events := convertPreviewTestPayloads(converter, batch, []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_test"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"raw-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":""}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"thinking-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_stop"},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"message-stop"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"orphan"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-delta"}`),
	})

	if len(events) != 2 {
		t.Fatalf("preview event count = %d, want two block starts: %#v", len(events), events)
	}
	if converter.states.Len() != 0 {
		t.Fatalf("state after message stop = %d, want zero", converter.states.Len())
	}
}

func TestWorkerPreviewConverterExpiresBlockLazilyWithoutDroppingActiveMessage(t *testing.T) {
	converter := newWorkerPreviewConverter()
	now := time.Now().UTC()
	scope := previewScope{codeSessionID: "cse_test", workerEpoch: 1, rawSessionID: "raw-session"}
	converter.states.Add(scope, &previewState{
		messageID: "msg_test",
		updatedAt: now,
		blocks: map[int]previewBlock{
			0: {eventID: "expired", eventType: "agent.message", updatedAt: now.Add(-previewCacheTTL - time.Second)},
			1: {eventID: "active", eventType: "agent.message", updatedAt: now},
		},
	})

	state, exists := converter.activePreviewState(scope, now)
	if !exists {
		t.Fatal("active message state expired with an expired sibling block")
	}
	if _, exists := state.blocks[0]; exists {
		t.Fatal("expired block was not removed on access")
	}
	if _, exists := state.blocks[1]; !exists || len(state.blocks) != 1 {
		t.Fatalf("active block state = exists:%t total:%d, want true and 1", exists, len(state.blocks))
	}
}

func TestWorkerPreviewConverterExpiresDedupEntryLazily(t *testing.T) {
	converter := newWorkerPreviewConverter()
	batch := previewTestBatch()
	payload := workerStreamPayload{UUID: "event-test"}
	now := time.Now().UTC()

	if converter.isDuplicate(batch, payload, now) {
		t.Fatal("first event was treated as duplicate")
	}
	if !converter.isDuplicate(batch, payload, now.Add(previewCacheTTL)) {
		t.Fatal("event at dedup expiry boundary was not treated as duplicate")
	}
	if converter.isDuplicate(batch, payload, now.Add(previewCacheTTL+time.Nanosecond)) {
		t.Fatal("expired dedup entry was treated as duplicate")
	}
}

func TestWorkerPreviewConverterResetSessionKeepsOtherSessions(t *testing.T) {
	converter := newWorkerPreviewConverter()
	firstScope := previewScope{sessionExternalID: "session-first", codeSessionID: "cse-first", workerEpoch: 1}
	secondScope := previewScope{sessionExternalID: "session-second", codeSessionID: "cse-second", workerEpoch: 1}
	converter.states.Add(firstScope, &previewState{})
	converter.states.Add(secondScope, &previewState{})
	firstSeen := seenStreamEventKey{sessionExternalID: "session-first", codeSessionID: "cse-first", workerEpoch: 1, eventUUID: "first"}
	secondSeen := seenStreamEventKey{sessionExternalID: "session-second", codeSessionID: "cse-second", workerEpoch: 1, eventUUID: "second"}
	converter.seen.Add(firstSeen, time.Now().UTC())
	converter.seen.Add(secondSeen, time.Now().UTC())

	converter.resetSession("session-first")

	if converter.states.Contains(firstScope) || converter.seen.Contains(firstSeen) {
		t.Fatal("reset session state was retained")
	}
	if !converter.states.Contains(secondScope) || !converter.seen.Contains(secondSeen) {
		t.Fatal("reset removed another session's preview state")
	}
}

func TestWorkerPreviewConverterEvictsOldestMessageAtCapacity(t *testing.T) {
	converter := newWorkerPreviewConverter()
	now := time.Now().UTC()
	oldestScope := previewScope{codeSessionID: "cse_test", workerEpoch: 1, rawSessionID: "oldest"}
	converter.startPreviewMessage(oldestScope, "msg_oldest", now)
	for workerEpoch := int64(2); workerEpoch <= maxPreviewMessages; workerEpoch++ {
		converter.startPreviewMessage(previewScope{codeSessionID: "cse_test", workerEpoch: workerEpoch}, "msg_test", now)
	}
	converter.startPreviewMessage(previewScope{codeSessionID: "cse_test", workerEpoch: maxPreviewMessages + 1}, "msg_newest", now)

	if converter.states.Contains(oldestScope) {
		t.Fatal("oldest message state was retained beyond LRU capacity")
	}
	if converter.states.Len() != maxPreviewMessages {
		t.Fatalf("message state count = %d, want %d", converter.states.Len(), maxPreviewMessages)
	}
}

func TestWorkerPreviewConverterFiltersNonPreviewStreamVariantsBeforeDedup(t *testing.T) {
	converter := newWorkerPreviewConverter()
	first := previewTestBatch()
	firstEvents := convertPreviewTestPayloads(converter, first, []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_test","content":[]}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"message-start","ttft_ms":5821}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"thinking-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"The"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"thinking-delta"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signature"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"signature-delta"}`),
	})
	if len(firstEvents) != 1 {
		t.Fatalf("first preview event count = %d, want thinking start: %#v", len(firstEvents), firstEvents)
	}

	second := previewTestBatch()
	secondEvents := convertPreviewTestPayloads(converter, second, []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_stop","index":0},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"thinking-stop"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_test","name":"Bash","input":{}}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"tool-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"input-json-delta"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_stop","index":1},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"tool-stop"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"text","text":"","citations":null}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"citations_delta","citation":{"type":"page_location","cited_text":"source"}}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"citation-delta"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Hi"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-delta"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"future_event","future_field":true},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"future-event"}`),
	})
	if len(secondEvents) != 2 {
		t.Fatalf("second preview event count = %d, want text start and delta: %#v", len(secondEvents), secondEvents)
	}
	wantTextID := managedagentsevents.StableAssistantEventID(second.CodeSessionID, "msg_test", 2, "agent.message")
	assertPreviewStart(t, secondEvents[0].Payload, "agent.message", wantTextID)
	assertPreviewDelta(t, secondEvents[1].Payload, wantTextID, "Hi")

	last := previewTestBatch()
	lastEvents := convertPreviewTestPayloads(converter, last, []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"message-delta"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_stop"},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"message-stop"}`),
	})
	if len(lastEvents) != 0 {
		t.Fatalf("terminal preview events = %#v, want none", lastEvents)
	}
	if converter.states.Len() != 0 {
		t.Fatalf("state count after message stop = %d, want zero", converter.states.Len())
	}

	for _, eventUUID := range []string{
		"thinking-delta",
		"signature-delta",
		"tool-start",
		"input-json-delta",
		"citation-delta",
		"future-event",
		"message-delta",
	} {
		key := seenStreamEventKey{codeSessionID: first.CodeSessionID, workerEpoch: first.WorkerEpoch, eventUUID: eventUUID}
		if seen := converter.seen.Contains(key); seen {
			t.Errorf("non-preview stream event %q entered dedup state", eventUUID)
		}
	}
}

func TestWorkerPreviewConverterEmitsThinkingStartWithoutDeltas(t *testing.T) {
	converter := newWorkerPreviewConverter()
	batch := previewTestBatch()
	events := convertPreviewTestPayloads(converter, batch, []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_test"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"raw-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"thinking-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"The"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"thinking-one"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" user"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"thinking-two"}`),
	})

	if len(events) != 1 {
		t.Fatalf("preview event count = %d, want 1: %#v", len(events), events)
	}
	wantID := managedagentsevents.StableAssistantEventID(batch.CodeSessionID, "msg_test", 0, "agent.thinking")
	assertPreviewStart(t, events[0].Payload, "agent.thinking", wantID)
}

func TestWorkerPreviewConverterForwardsTextFragmentsAcrossBatches(t *testing.T) {
	converter := newWorkerPreviewConverter()
	first := previewTestBatch()
	firstEvents := convertPreviewTestPayloads(converter, first, []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_test"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"raw-start"}`),
	})
	if len(firstEvents) != 0 {
		t.Fatalf("message_start events = %#v, want none", firstEvents)
	}
	second := previewTestBatch()
	payloads := []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hello"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-one"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":" world"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-two"}`),
	}

	events := convertPreviewTestPayloads(converter, second, payloads)
	if len(events) != 3 {
		t.Fatalf("preview event count = %d, want 3: %#v", len(events), events)
	}
	wantID := managedagentsevents.StableAssistantEventID(second.CodeSessionID, "msg_test", 1, "agent.message")
	assertPreviewStart(t, events[0].Payload, "agent.message", wantID)
	assertPreviewDelta(t, events[1].Payload, wantID, "Hello")
	assertPreviewDelta(t, events[2].Payload, wantID, " world")
	for index, event := range events {
		if event.CreatedAt.IsZero() || event.ProcessedAt.IsZero() {
			t.Fatalf("preview event %d times = created:%v processed:%v, want non-zero", index, event.CreatedAt, event.ProcessedAt)
		}
	}

	if duplicates := convertPreviewTestPayloads(converter, second, payloads); len(duplicates) != 0 {
		t.Fatalf("duplicate worker events produced previews: %#v", duplicates)
	}
}

func TestPreviewSessionEventDistinguishesPrimaryAndChildScopes(t *testing.T) {
	batch := previewTestBatch()
	processedAt := time.Date(2026, time.August, 20, 1, 2, 3, 0, time.UTC)
	primary := previewSessionEvent(batch, workerStreamPayload{}, processedAt, "primary-event", "event_start", json.RawMessage(`{}`))
	if !primary.PrimaryThread || primary.ThreadExternalID != nil {
		t.Fatalf("primary preview routing = %#v, want primary scope without physical thread id", primary)
	}

	parentToolUseID := "tool-parent"
	child := previewSessionEvent(batch, workerStreamPayload{ParentToolUseID: &parentToolUseID}, processedAt, "child-event", "event_start", json.RawMessage(`{}`))
	wantThreadID := managedagentsevents.ClaudeTaskThreadID(batch.CodeSessionID, parentToolUseID)
	if child.PrimaryThread || child.ThreadExternalID == nil || *child.ThreadExternalID != wantThreadID {
		t.Fatalf("child preview routing = %#v, want child thread %q", child, wantThreadID)
	}
}

func TestPreviewSSEIncludesTimesAndResolvedPrimaryThread(t *testing.T) {
	createdAt := time.Date(2026, time.August, 20, 1, 2, 3, 0, time.UTC)
	processedAt := createdAt.Add(2 * time.Second)
	event := previewSessionEvent(
		previewTestBatch(),
		workerStreamPayload{CreatedAt: createdAt.Format(time.RFC3339Nano)},
		processedAt,
		"preview-event",
		previewEventStart,
		json.RawMessage(`{"type":"event_start"}`),
	)
	recorder := httptest.NewRecorder()

	writeSSE(recorder, event, "primary-thread")

	data := strings.TrimSpace(strings.TrimPrefix(strings.Split(recorder.Body.String(), "\n")[1], "data: "))
	var payload struct {
		CreatedAt       string `json:"created_at"`
		ProcessedAt     string `json:"processed_at"`
		SessionThreadID string `json:"session_thread_id"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("decode preview SSE data: %v", err)
	}
	if payload.CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("created_at = %q, want %q", payload.CreatedAt, createdAt.Format(time.RFC3339Nano))
	}
	if payload.ProcessedAt != processedAt.Format(time.RFC3339Nano) {
		t.Fatalf("processed_at = %q, want %q", payload.ProcessedAt, processedAt.Format(time.RFC3339Nano))
	}
	if payload.SessionThreadID != "primary-thread" {
		t.Fatalf("session_thread_id = %q, want primary-thread", payload.SessionThreadID)
	}
}

func TestSessionFanoutConvertsWorkerStreamOncePerInstance(t *testing.T) {
	bus := sessionfanout.NewLocal()
	publisher := newFanoutTestHandler(bus)
	receiver := newFanoutTestHandler(bus)
	_, firstCh := receiver.streams.subscribe("workspace-test", "session-test")
	_, secondCh := receiver.streams.subscribe("workspace-test", "session-test")
	_, childCh := receiver.streams.subscribe("workspace-test", "session-test")
	first := newStreamConnection("thread-test", true, map[string]struct{}{"agent.message": {}})
	second := newStreamConnection("thread-test", true, map[string]struct{}{"agent.message": {}})
	child := newStreamConnection("child-thread-test", false, map[string]struct{}{"agent.message": {}})
	primaryStreams := []struct {
		connection *streamConnection
		ch         <-chan streamDelivery
	}{
		{connection: first, ch: firstCh},
		{connection: second, ch: secondCh},
	}

	batch := previewTestBatch()
	payloads := []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_test"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"raw-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}},"session_id":"raw-session","parent_tool_use_id":null,"uuid":"text-delta"}`),
	}
	for _, payload := range payloads {
		batch.Payload = payload
		if err := publisher.publishFanout(context.Background(), batch.SessionExternalID, sessionfanout.KindCodeSessionStream, batch); err != nil {
			t.Fatalf("publish fanout: %v", err)
		}
	}

	for index, stream := range primaryStreams {
		events := collectConnectionEvents(t, stream.connection, stream.ch, 2)
		if len(events) != 2 || events[0].EventType != previewEventStart || events[1].EventType != previewEventDelta {
			t.Fatalf("connection %d preview events = %#v, want start and delta", index, events)
		}
	}
	if events := collectConnectionEvents(t, child, childCh, 2); len(events) != 0 {
		t.Fatalf("primary preview leaked to child connection: %#v", events)
	}
	if seen := receiver.previews.seen.Len(); seen != 3 {
		t.Fatalf("instance preview dedup state = %d, want one three-event batch", seen)
	}
}

func TestStreamConnectionResetDropsOrphanDelta(t *testing.T) {
	connection := newStreamConnection("thread-test", true, map[string]struct{}{"agent.message": {}})
	block := previewBlock{eventID: "event-test", eventType: "agent.message"}
	start := sessionStreamEvent{
		ExternalID:    block.eventID,
		PrimaryThread: true,
		EventType:     previewEventStart,
		Payload:       eventStartPayload(block),
	}
	if _, accepted := connection.event(sessionEventDelivery{event: start}); !accepted {
		t.Fatal("preview start was not accepted")
	}
	connection.event(streamResetDelivery{})
	delta := sessionStreamEvent{
		ExternalID:    block.eventID,
		PrimaryThread: true,
		EventType:     previewEventDelta,
		Payload:       eventDeltaPayload(block.eventID, "orphan"),
	}
	if _, accepted := connection.event(sessionEventDelivery{event: delta}); accepted {
		t.Fatal("orphan delta was accepted after reset")
	}
}

func TestStreamConnectionAcceptsLegacyDeltaWithoutPreviewID(t *testing.T) {
	connection := newStreamConnection("thread-test", true, map[string]struct{}{"agent.message": {}})
	event := sessionStreamEvent{
		ExternalID:    "legacy-event-test",
		PrimaryThread: true,
		EventType:     previewEventDelta,
		Payload:       json.RawMessage(`{"type":"event_delta","delta":{"text":"legacy"}}`),
	}
	if _, accepted := connection.event(sessionEventDelivery{event: event}); !accepted {
		t.Fatal("legacy stream delta without preview ID was not accepted")
	}
}

func TestRequestedStreamDeltaTypesValidatesValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/events/stream?event_deltas%5B%5D=agent.message&event_deltas%5B%5D=other", nil)
	if _, err := requestedStreamDeltaTypes(req); err == nil {
		t.Fatal("requestedStreamDeltaTypes() error = nil, want invalid type error")
	}

	values := ""
	for i := 0; i < 101; i++ {
		values += "&event_deltas%5B%5D=agent.message"
	}
	req = httptest.NewRequest("GET", "/events/stream?"+values[1:], nil)
	if _, err := requestedStreamDeltaTypes(req); err == nil {
		t.Fatal("requestedStreamDeltaTypes() error = nil, want value limit error")
	}
}

func previewTestBatch() codeSessionStreamFanout {
	return codeSessionStreamFanout{
		WorkspaceUUID:     "workspace-test",
		SessionExternalID: "session-test",
		CodeSessionID:     "cse_test",
		WorkerEpoch:       1,
	}
}

func convertPreviewTestPayloads(converter *workerPreviewConverter, message codeSessionStreamFanout, payloads []json.RawMessage) []sessionStreamEvent {
	events := make([]sessionStreamEvent, 0, len(payloads))
	for _, payload := range payloads {
		message.Payload = payload
		if event, emit := converter.convert(message); emit {
			events = append(events, event)
		}
	}
	return events
}

func newFanoutTestHandler(bus sessionfanout.EventBus) *Handler {
	handler := &Handler{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		streams:  newStreamHub(),
		eventBus: bus,
		previews: newWorkerPreviewConverter(),
	}
	bus.Register(handler.receiveFanout, handler.resetFanout)
	return handler
}

func collectConnectionEvents(t *testing.T, connection *streamConnection, ch <-chan streamDelivery, deliveryCount int) []sessionStreamEvent {
	t.Helper()
	events := make([]sessionStreamEvent, 0, deliveryCount)
	for range deliveryCount {
		if event, accepted := connection.event(awaitStreamDelivery(t, ch)); accepted {
			events = append(events, event)
		}
	}
	return events
}

func awaitStreamDelivery(t *testing.T, ch <-chan streamDelivery) streamDelivery {
	t.Helper()
	select {
	case delivery := <-ch:
		return delivery
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream delivery")
		return nil
	}
}

func assertPreviewStart(t *testing.T, raw json.RawMessage, eventType, eventID string) {
	t.Helper()
	var payload struct {
		Type  string `json:"type"`
		Event struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode event_start: %v", err)
	}
	if payload.Type != "event_start" || payload.Event.Type != eventType || payload.Event.ID != eventID {
		t.Fatalf("event_start = %#v, want type=%q id=%q", payload, eventType, eventID)
	}
}

func assertPreviewDelta(t *testing.T, raw json.RawMessage, eventID, text string) {
	t.Helper()
	var payload struct {
		Type    string `json:"type"`
		EventID string `json:"event_id"`
		Delta   struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode event_delta: %v", err)
	}
	if payload.Type != "event_delta" || payload.EventID != eventID || payload.Delta.Type != "content_delta" || payload.Delta.Index != 0 || payload.Delta.Content.Type != "text" || payload.Delta.Content.Text != text {
		t.Fatalf("event_delta = %#v, want id=%q text=%q", payload, eventID, text)
	}
}
