package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/go-chi/chi/v5"
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
	streamDeltaTypes   map[string]struct{}
	activePreviewIDs   map[string]struct{}
	finishedPreviewIDs map[string]struct{}
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
	case "span.model_request_end":
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
	h.streamEvents(w, r, chi.URLParam(r, "session_id"), "", true)
}

func (h *Handler) StreamEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	h.streamEvents(w, r, sessionID, "", true)
}

func (h *Handler) streamThreadEventsRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")

	if _, err := h.authorizeSession(r, sessionID, sessionAccessEventsRead); err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	thread, err := h.db.GetSessionThread(r.Context(), workspaceUUIDFromRequest(r), sessionID, threadID)
	if err != nil {
		h.errorAdapter.Write(w, r, mapThreadLoadError(err, threadID))
		return
	}
	h.streamEvents(w, r, sessionID, threadID, thread.ParentThreadUUID == nil)
}

func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request, sessionID, threadID string, primaryThread bool) {
	session, err := h.authorizeSession(r, sessionID, sessionAccessEventsRead)
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return
	}
	streamDeltaTypes, err := requestedStreamDeltaTypes(r)
	if err != nil {
		h.errorAdapter.Write(w, r, invalidRequest(err))
		return
	}
	subscribeThreadID := threadID
	if subscribeThreadID == "" {
		primary, err := h.ensurePrimarySessionThread(r.Context(), session)
		if err != nil {
			h.errorAdapter.Write(w, r, mapSessionLoadError(err, sessionID))
			return
		}
		subscribeThreadID = primary.ExternalID
		primaryThread = true
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.errorAdapter.Write(w, r, streamingUnsupported())
		return
	}
	scope := db.SessionEventStreamParams{
		WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: sessionID,
		ThreadExternalID: threadID, PrimaryOnly: threadID == "",
	}
	lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if len(lastEventID) > 256 {
		h.errorAdapter.Write(w, r, invalidRequest(errors.New("Last-Event-ID is invalid; reconnect without it")))
		return
	}
	position, err := h.db.SessionEventStreamPosition(r.Context(), scope, lastEventID)
	if errors.Is(err, db.ErrInvalidCursor) {
		h.errorAdapter.Write(w, r, invalidRequest(errors.New("Last-Event-ID is invalid; reconnect without it")))
		return
	}
	if err != nil {
		h.errorAdapter.Write(w, r, internalError("Could not locate session event stream cursor", err))
		return
	}
	// Capture the database waterline before subscribing. Events committed in the
	// gap are recovered by the first catch-up query, and queued notifications
	// after subscription can safely trigger the same query again.
	subID, ch := h.streams.subscribe(session.WorkspaceUUID, sessionID)
	if err := h.eventBus.Subscribe(r.Context(), session.ExternalID); err != nil {
		h.streams.unsubscribe(subID)
		h.errorAdapter.Write(w, r, internalError("Could not subscribe to session event stream", err))
		return
	}
	defer func() {
		h.streams.unsubscribe(subID)
		if err := h.eventBus.Unsubscribe(session.ExternalID); err != nil {
			h.logger.WarnContext(r.Context(), "unsubscribe session event stream", "session_id", session.ExternalID, "error", err)
		}
	}()
	connection := newStreamConnection(subscribeThreadID, primaryThread, streamDeltaTypes)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	catchUp := func() bool {
		if err := h.catchUpSessionStream(r, w, flusher, connection, scope, subscribeThreadID, &position); err != nil {
			if r.Context().Err() == nil {
				h.logger.ErrorContext(r.Context(), "replay session event stream", "session_id", sessionID, "error", err)
			}
			return false
		}
		return true
	}
	if !catchUp() {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !catchUp() {
				return
			}
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case delivery, ok := <-ch:
			if !ok {
				return
			}
			if persisted, ok := delivery.(sessionEventDelivery); ok && resumableSessionEvent(persisted.event.EventType) {
				if !catchUp() {
					return
				}
				continue
			}
			event, accepted := connection.event(delivery)
			if accepted {
				writeSSE(w, event, subscribeThreadID)
				flusher.Flush()
			}
		}
	}
}

func (h *Handler) catchUpSessionStream(r *http.Request, w http.ResponseWriter, flusher http.Flusher, connection *streamConnection, scope db.SessionEventStreamParams, threadID string, position *int64) error {
	const pageSize = 200
	for {
		page, err := h.db.ListSessionStreamEventsPage(r.Context(), scope, *position, pageSize)
		if err != nil {
			return err
		}
		page, err = h.eventPayloads.RestorePublicPage(r.Context(), page)
		if err != nil {
			return err
		}
		for _, record := range page {
			*position = record.DeliverySeq
			event := sessionStreamEventFrom(record)
			if connection.accepts(event) {
				writeSSE(w, event, threadID)
				flusher.Flush()
			}
		}
		if len(page) < pageSize {
			return nil
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
	if resumableSessionEvent(event.EventType) {
		fmt.Fprintf(w, "id: %s\n", event.ExternalID)
	}
	fmt.Fprintf(w, "event: %s\n", event.EventType)
	if maevents.IsStreamDelta(event.EventType) {
		fmt.Fprintf(w, "data: %s\n\n", event.Payload)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", eventPayloadForResponse(event.Payload, event.CreatedAt, event.ProcessedAt, threadID))
}

func resumableSessionEvent(eventType string) bool {
	return maevents.IsPublicSessionHistoryEvent(eventType) && eventType != "session.deleted"
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
