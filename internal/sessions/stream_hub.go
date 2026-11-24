package sessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type streamHub struct {
	mu          sync.Mutex
	nextSubID   int64
	subscribers map[int64]*subscriber
}

type subscriber struct {
	workspaceUUID string
	sessionID     string
	ch            chan streamDelivery
}

// streamDelivery is implemented by each delivery variant consumed by an SSE
// connection.
type streamDelivery interface {
	implStreamDelivery()
}

type sessionEventDelivery struct {
	event sessionStreamEvent
}

type streamResetDelivery struct{}

func (sessionEventDelivery) implStreamDelivery() {}
func (streamResetDelivery) implStreamDelivery()  {}

type streamConnection struct {
	threadID           string
	primaryThread      bool
	sessionStream      bool
	streamDeltaTypes   map[string]struct{}
	activePreviewIDs   map[string]struct{}
	finishedPreviewIDs map[string]struct{}
	replayedEventIDs   map[string]bool
}

func newStreamHub() *streamHub {
	return &streamHub{subscribers: map[int64]*subscriber{}}
}

func (h *streamHub) subscribe(workspaceUUID, sessionID string) (int64, <-chan streamDelivery) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSubID++
	id := h.nextSubID
	ch := make(chan streamDelivery, 256)
	h.subscribers[id] = &subscriber{
		workspaceUUID: workspaceUUID,
		sessionID:     sessionID,
		ch:            ch,
	}
	return id, ch
}

func (h *streamHub) unsubscribe(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sub, ok := h.subscribers[id]; ok {
		delete(h.subscribers, id)
		close(sub.ch)
	}
}

func (h *streamHub) broadcastEvent(event sessionStreamEvent) {
	if !maevents.IsPublicSessionHistoryEvent(event.EventType) && !maevents.IsStreamDelta(event.EventType) {
		return
	}
	h.broadcastToSession(event.WorkspaceUUID, event.SessionExternalID, sessionEventDelivery{event: event})
}

func (h *streamHub) broadcastToSession(workspaceUUID, sessionID string, delivery streamDelivery) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, sub := range h.subscribers {
		if sub.workspaceUUID != workspaceUUID || sub.sessionID != sessionID {
			continue
		}
		h.enqueue(id, sub, delivery)
	}
}

func (h *streamHub) resetSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, sub := range h.subscribers {
		if sub.sessionID != sessionID {
			continue
		}
		h.enqueue(id, sub, streamResetDelivery{})
	}
}

func (h *streamHub) enqueue(id int64, sub *subscriber, delivery streamDelivery) {
	select {
	case sub.ch <- delivery:
	default:
		sessionMetrics.Get().sseSubscribersDropped.Add(context.Background(), 1)
		delete(h.subscribers, id)
		close(sub.ch)
	}
}

func newStreamConnection(threadID string, primaryThread bool, streamDeltaTypes map[string]struct{}) *streamConnection {
	return &streamConnection{
		threadID:           threadID,
		primaryThread:      primaryThread,
		streamDeltaTypes:   streamDeltaTypes,
		activePreviewIDs:   make(map[string]struct{}),
		finishedPreviewIDs: make(map[string]struct{}),
	}
}

func (c *streamConnection) event(delivery streamDelivery) (sessionStreamEvent, bool) {
	switch delivery := delivery.(type) {
	case streamResetDelivery:
		clear(c.activePreviewIDs)
		return sessionStreamEvent{}, false
	case sessionEventDelivery:
		return delivery.event, c.accepts(delivery.event)
	}
	return sessionStreamEvent{}, false
}

func (c *streamConnection) accepts(event sessionStreamEvent) bool {
	if !c.matches(event) {
		return false
	}
	if maevents.IsPublicSessionHistoryEvent(event.EventType) {
		if processed, replayed := c.replayedEventIDs[event.ExternalID]; replayed {
			mutableInput := event.EventType == "user.message" || event.EventType == "user.tool_confirmation" || event.EventType == "user.interrupt"
			if !mutableInput || processed || event.ProcessedAt.IsZero() {
				return false
			}
			c.replayedEventIDs[event.ExternalID] = true
		}
		delete(c.activePreviewIDs, event.ExternalID)
		// Only preview subscribers can receive late deltas, so only they remember finished previews.
		if len(c.streamDeltaTypes) > 0 {
			for _, id := range previewsFinishedBy(event) {
				c.finishedPreviewIDs[id] = struct{}{}
				delete(c.activePreviewIDs, id)
			}
		}
		return true
	}
	if !maevents.IsStreamDelta(event.EventType) {
		return false
	}
	if len(c.streamDeltaTypes) == 0 {
		return false
	}
	previewType, previewID := streamPreviewTarget(event)
	if _, finished := c.finishedPreviewIDs[previewID]; finished {
		return false
	}
	if event.EventType == previewEventDelta && previewID == "" {
		// Legacy event_delta payloads predate preview lifecycle IDs. They cannot
		// be correlated with an event_start, so retain their previous opt-in
		// delivery behavior for clients requesting stream deltas.
		return true
	}
	if event.EventType == previewEventStart {
		if !c.acceptsPreviewType(previewType) {
			return false
		}
		if _, active := c.activePreviewIDs[previewID]; active {
			return false
		}
		c.activePreviewIDs[previewID] = struct{}{}
		return true
	}
	_, active := c.activePreviewIDs[previewID]
	return active
}

// A request end retires only its own previews, including ones whose start arrives after the end.
func previewsFinishedBy(event sessionStreamEvent) []string {
	switch event.EventType {
	case "agent.message", "agent.thinking":
		return []string{event.ExternalID}
	case "span.model_request_end", "system.message":
		var end struct {
			EventIDs []string `json:"event_ids"`
		}
		if json.Unmarshal(event.Payload, &end) != nil {
			return nil
		}
		return end.EventIDs
	default:
		return nil
	}
}

func (c *streamConnection) matches(event sessionStreamEvent) bool {
	if event.EventType == "session.thread_status_idle" {
		var idle struct {
			ThreadID        string `json:"session_thread_id"`
			OriginalEventID string `json:"original_event_id"`
		}
		if jsonv2.Unmarshal(event.Payload, &idle) != nil || idle.OriginalEventID != "" {
			return false
		}
		if c.sessionStream {
			return true
		}
		if idle.ThreadID != "" {
			return idle.ThreadID == c.threadID
		}
	}
	if event.PrimaryThread {
		return c.primaryThread
	}
	return event.ThreadExternalID != nil && *event.ThreadExternalID == c.threadID
}

func (c *streamConnection) acceptsPreviewType(eventType string) bool {
	_, ok := c.streamDeltaTypes[eventType]
	return ok
}

func (h *Handler) streamEventsRoute(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, chi.URLParam(r, "session_id"), "")
}

func (h *Handler) StreamEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	h.streamEvents(w, r, sessionID, "")
}

func (h *Handler) streamThreadEventsRoute(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, chi.URLParam(r, "session_id"), chi.URLParam(r, "thread_id"))
}

func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request, sessionID, threadID string) {
	ctx, cancel := httpapi.StreamContext(r.Context())
	defer cancel()
	r = r.WithContext(ctx)
	deltaTypes, err := requestedStreamDeltaTypes(r)
	if err != nil {
		h.errorAdapter.Write(w, r, invalidRequest(err))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.errorAdapter.Write(w, r, streamingUnsupported())
		return
	}
	subscription, err := h.SubscribeEvents(ctx, sessionID, StreamOptions{ThreadID: threadID, AfterEventID: strings.TrimSpace(r.Header.Get("Last-Event-ID")), DeltaTypes: deltaTypes})
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	defer subscription.Close()
	defer httpapi.ObserveSSEStream(ctx, "public")()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		event, err := subscription.receive(ctx, ticker.C)
		if errors.Is(err, ErrStreamHeartbeat) {
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
			continue
		}
		if err != nil {
			return
		}
		writeSSE(w, event, subscription.threadID)
		flusher.Flush()
		if event.EventType == "session.deleted" {
			return
		}
	}
}

func requestedStreamDeltaTypes(r *http.Request) (map[string]struct{}, error) {
	values := parseRepeatedQuery(r, "event_deltas[]", "event_deltas")
	if len(values) > 100 {
		return nil, errors.New("event_deltas may contain at most 100 values")
	}
	types := make(map[string]struct{}, len(values))
	for _, value := range values {
		switch value {
		case "agent.message", "agent.thinking":
			types[value] = struct{}{}
		default:
			return nil, errors.New("event_deltas must contain agent.message or agent.thinking")
		}
	}
	return types, nil
}

func writeSSE(w http.ResponseWriter, event sessionStreamEvent, threadID string) {
	isPreview := maevents.IsStreamDelta(event.EventType)
	eventID := event.ExternalID
	if isPreview {
		_, eventID = streamPreviewTarget(event)
	}
	if (isPreview || maevents.IsPublicSessionHistoryEvent(event.EventType)) && eventID != "" && !strings.ContainsAny(eventID, "\r\n\x00") {
		fmt.Fprintf(w, "id: %s\n", eventID)
	} else if isPreview {
		fmt.Fprint(w, "id:\n")
	}
	fmt.Fprintf(w, "event: %s\n", event.EventType)
	if isPreview {
		fmt.Fprintf(w, "data: %s\n\n", event.Payload)
		return
	}
	if event.EventType == "session.thread_status_idle" && event.ThreadExternalID != nil {
		threadID = *event.ThreadExternalID
	}
	fmt.Fprintf(w, "data: %s\n\n", eventPayloadForResponse(event.Payload, event.ProcessedAt, threadID))
}

func streamPreviewTarget(event sessionStreamEvent) (string, string) {
	var payload struct {
		Event struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"event"`
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return "", ""
	}
	if event.EventType == "event_start" {
		return strings.TrimSpace(payload.Event.Type), strings.TrimSpace(payload.Event.ID)
	}
	return "", strings.TrimSpace(payload.EventID)
}
