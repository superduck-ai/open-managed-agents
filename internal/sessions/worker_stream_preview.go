package sessions

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

const (
	previewCacheTTL                    = 30 * time.Minute
	maxPreviewMessages                 = 10_000
	maxSeenStreamEvents                = 100_000
	previewEventStart                  = "event_start"
	previewEventDelta                  = "event_delta"
	workerStreamPayloadTypeStreamEvent = "stream_event"
	workerStreamContentBlockThinking   = "thinking"
	workerStreamContentBlockText       = "text"
	workerStreamDeltaText              = "text_delta"
)

type workerPreviewConverter struct {
	mu     sync.Mutex
	states *simplelru.LRU[previewScope, *previewState]
	seen   *simplelru.LRU[seenStreamEventKey, time.Time]
}

type previewScope struct {
	sessionExternalID string
	codeSessionID     string
	workerEpoch       int64
	rawSessionID      string
	parentToolUseID   string
}

type seenStreamEventKey struct {
	sessionExternalID string
	codeSessionID     string
	workerEpoch       int64
	eventUUID         string
}

type previewBlock struct {
	eventID   string
	eventType string
	updatedAt time.Time
}

type previewState struct {
	messageID string
	blocks    map[int]previewBlock
	updatedAt time.Time
}

type workerStreamPayload struct {
	Type            string            `json:"type"`
	Event           workerStreamEvent `json:"event"`
	SessionID       string            `json:"session_id"`
	ParentToolUseID *string           `json:"parent_tool_use_id"`
	UUID            string            `json:"uuid"`
	CreatedAt       string            `json:"created_at"`
	Timestamp       string            `json:"timestamp"`
}

type workerStreamEventType string

const (
	workerStreamEventTypeMessageStart      workerStreamEventType = "message_start"
	workerStreamEventTypeContentBlockStart workerStreamEventType = "content_block_start"
	workerStreamEventTypeContentBlockDelta workerStreamEventType = "content_block_delta"
	workerStreamEventTypeContentBlockStop  workerStreamEventType = "content_block_stop"
	workerStreamEventTypeMessageStop       workerStreamEventType = "message_stop"
)

type workerStreamEvent struct {
	Type         workerStreamEventType    `json:"type"`
	Index        *int                     `json:"index"`
	Message      workerStreamMessage      `json:"message"`
	ContentBlock workerStreamContentBlock `json:"content_block"`
	Delta        workerStreamDelta        `json:"delta"`
}

type workerStreamMessage struct {
	ID string `json:"id"`
}

type workerStreamContentBlock struct {
	Type string `json:"type"`
}

type workerStreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func newWorkerPreviewConverter() *workerPreviewConverter {
	return &workerPreviewConverter{
		states: newWorkerPreviewLRU[previewScope, *previewState](maxPreviewMessages),
		seen:   newWorkerPreviewLRU[seenStreamEventKey, time.Time](maxSeenStreamEvents),
	}
}

func newWorkerPreviewLRU[K comparable, V any](size int) *simplelru.LRU[K, V] {
	cache, err := simplelru.NewLRU[K, V](size, nil)
	if err != nil {
		panic(fmt.Sprintf("create worker preview cache: %v", err))
	}
	return cache
}

func (c *workerPreviewConverter) convert(message codeSessionStreamFanout) (sessionStreamEvent, bool) {
	payload, ok := decodeWorkerStreamPayload(message.Payload)
	if !ok || !payload.Event.affectsPreview() {
		return sessionStreamEvent{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	if c.isDuplicate(message, payload, now) {
		return sessionStreamEvent{}, false
	}
	return c.applyStreamEvent(message, payload, now)
}

func decodeWorkerStreamPayload(raw json.RawMessage) (workerStreamPayload, bool) {
	var payload workerStreamPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Type != workerStreamPayloadTypeStreamEvent {
		return workerStreamPayload{}, false
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.CreatedAt = strings.TrimSpace(payload.CreatedAt)
	payload.Timestamp = strings.TrimSpace(payload.Timestamp)
	if payload.ParentToolUseID != nil {
		*payload.ParentToolUseID = strings.TrimSpace(*payload.ParentToolUseID)
	}
	payload.Event.Message.ID = strings.TrimSpace(payload.Event.Message.ID)
	payload.Event.ContentBlock.Type = strings.TrimSpace(payload.Event.ContentBlock.Type)
	return payload, true
}

func (event workerStreamEvent) affectsPreview() bool {
	switch event.Type {
	case workerStreamEventTypeMessageStart:
		return event.Message.ID != ""
	case workerStreamEventTypeContentBlockStart:
		return event.Index != nil && publicPreviewEventType(event.ContentBlock.Type) != ""
	case workerStreamEventTypeContentBlockDelta:
		return event.Index != nil && event.Delta.Type == workerStreamDeltaText && event.Delta.Text != ""
	case workerStreamEventTypeContentBlockStop:
		return event.Index != nil
	case workerStreamEventTypeMessageStop:
		return true
	default:
		return false
	}
}

func (c *workerPreviewConverter) isDuplicate(batch codeSessionStreamFanout, payload workerStreamPayload, now time.Time) bool {
	if payload.UUID == "" {
		return false
	}
	key := seenStreamEventKey{
		sessionExternalID: batch.SessionExternalID,
		codeSessionID:     batch.CodeSessionID,
		workerEpoch:       batch.WorkerEpoch,
		eventUUID:         payload.UUID,
	}
	expiresAt, seen := c.seen.Get(key)
	if seen && !expiresAt.Before(now) {
		return true
	}
	c.seen.Add(key, now.Add(previewCacheTTL))
	return false
}

func (c *workerPreviewConverter) applyStreamEvent(batch codeSessionStreamFanout, payload workerStreamPayload, now time.Time) (sessionStreamEvent, bool) {
	scope := previewScope{
		sessionExternalID: batch.SessionExternalID,
		codeSessionID:     batch.CodeSessionID,
		workerEpoch:       batch.WorkerEpoch,
		rawSessionID:      payload.SessionID,
		parentToolUseID:   optionalString(payload.ParentToolUseID),
	}
	switch payload.Event.Type {
	case workerStreamEventTypeMessageStart:
		c.startPreviewMessage(scope, payload.Event.Message.ID, now)
	case workerStreamEventTypeContentBlockStart:
		return c.startPreviewBlock(batch, payload, scope, now)
	case workerStreamEventTypeContentBlockDelta:
		return c.appendPreviewBlockDelta(batch, payload, scope, now)
	case workerStreamEventTypeContentBlockStop:
		c.stopPreviewBlock(scope, *payload.Event.Index, now)
	case workerStreamEventTypeMessageStop:
		c.deletePreviewState(scope)
	}
	return sessionStreamEvent{}, false
}

func (c *workerPreviewConverter) resetSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, scope := range c.states.Keys() {
		if scope.sessionExternalID == sessionID {
			c.states.Remove(scope)
		}
	}
	for _, key := range c.seen.Keys() {
		if key.sessionExternalID == sessionID {
			c.seen.Remove(key)
		}
	}
}

func (c *workerPreviewConverter) startPreviewMessage(scope previewScope, messageID string, now time.Time) {
	c.deletePreviewState(scope)
	c.states.Add(scope, &previewState{
		messageID: messageID,
		blocks:    make(map[int]previewBlock),
		updatedAt: now,
	})
}

func (c *workerPreviewConverter) startPreviewBlock(batch codeSessionStreamFanout, payload workerStreamPayload, scope previewScope, now time.Time) (sessionStreamEvent, bool) {
	state, found := c.activePreviewState(scope, now)
	eventType := publicPreviewEventType(payload.Event.ContentBlock.Type)
	if !found {
		return sessionStreamEvent{}, false
	}
	index := *payload.Event.Index
	if _, exists := state.blocks[index]; exists {
		return sessionStreamEvent{}, false
	}
	block := previewBlock{
		eventID:   managedagentsevents.StableAssistantEventID(batch.CodeSessionID, state.messageID, index, eventType),
		eventType: eventType,
		updatedAt: now,
	}
	state.blocks[index] = block
	state.updatedAt = now
	return previewSessionEvent(batch, payload, now, block.eventID, previewEventStart, eventStartPayload(block)), true
}

func (c *workerPreviewConverter) appendPreviewBlockDelta(batch codeSessionStreamFanout, payload workerStreamPayload, scope previewScope, now time.Time) (sessionStreamEvent, bool) {
	state, found := c.activePreviewState(scope, now)
	if !found {
		return sessionStreamEvent{}, false
	}
	index := *payload.Event.Index
	block, found := state.blocks[index]
	if !found || block.eventType != "agent.message" {
		return sessionStreamEvent{}, false
	}
	block.updatedAt = now
	state.blocks[index] = block
	state.updatedAt = now
	payloadJSON := eventDeltaPayload(block.eventID, payload.Event.Delta.Text)
	return previewSessionEvent(batch, payload, now, block.eventID, previewEventDelta, payloadJSON), true
}

func (c *workerPreviewConverter) activePreviewState(scope previewScope, now time.Time) (*previewState, bool) {
	state, found := c.states.Get(scope)
	if !found {
		return nil, false
	}
	cutoff := now.Add(-previewCacheTTL)
	if state.updatedAt.Before(cutoff) {
		c.deletePreviewState(scope)
		return nil, false
	}
	for index, block := range state.blocks {
		if block.updatedAt.Before(cutoff) {
			delete(state.blocks, index)
		}
	}
	return state, true
}

func (c *workerPreviewConverter) stopPreviewBlock(scope previewScope, index int, now time.Time) {
	if state, found := c.activePreviewState(scope, now); found {
		if _, exists := state.blocks[index]; !exists {
			return
		}
		delete(state.blocks, index)
	}
}

func (c *workerPreviewConverter) deletePreviewState(scope previewScope) {
	c.states.Remove(scope)
}

func previewSessionEvent(batch codeSessionStreamFanout, source workerStreamPayload, processedAt time.Time, externalID, eventType string, payload json.RawMessage) sessionStreamEvent {
	event := sessionStreamEvent{
		ExternalID:        externalID,
		WorkspaceUUID:     batch.WorkspaceUUID,
		SessionExternalID: batch.SessionExternalID,
		PrimaryThread:     true,
		EventType:         eventType,
		Payload:           payload,
		ProcessedAt:       processedAt,
		CreatedAt:         source.previewCreatedAt(processedAt),
	}
	if parentToolUseID := optionalString(source.ParentToolUseID); parentToolUseID != "" {
		threadID := managedagentsevents.ClaudeTaskThreadID(batch.CodeSessionID, parentToolUseID)
		event.PrimaryThread = false
		event.ThreadExternalID = &threadID
	}
	return event
}

func (payload workerStreamPayload) previewCreatedAt(fallback time.Time) time.Time {
	for _, value := range []string{payload.CreatedAt, payload.Timestamp} {
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err == nil {
			return parsed.UTC()
		}
	}
	return fallback
}

func eventStartPayload(block previewBlock) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{
		"type": "event_start",
		"event": map[string]string{
			"type": block.eventType,
			"id":   block.eventID,
		},
	})
	return payload
}

func eventDeltaPayload(eventID, text string) json.RawMessage {
	payload, _ := json.Marshal(map[string]any{
		"type":     "event_delta",
		"event_id": eventID,
		"delta": map[string]any{
			"type":  "content_delta",
			"index": 0,
			"content": map[string]string{
				"type": "text",
				"text": text,
			},
		},
	})
	return payload
}

func publicPreviewEventType(blockType string) string {
	switch blockType {
	case workerStreamContentBlockThinking:
		return "agent.thinking"
	case workerStreamContentBlockText:
		return "agent.message"
	default:
		return ""
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
