package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

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
	threadID         string
	historyThreadID  string
	primaryThread    bool
	streamDeltaTypes map[string]struct{}
	activePreviewIDs map[string]string
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
		threadID:         threadID,
		primaryThread:    primaryThread,
		streamDeltaTypes: streamDeltaTypes,
		activePreviewIDs: make(map[string]string),
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
		return true
	}
	if !maevents.IsStreamDelta(event.EventType) || len(c.streamDeltaTypes) == 0 {
		return false
	}
	previewType, previewID := streamPreviewTarget(event)
	if previewID == "" {
		return false
	}
	if event.EventType == previewEventStart {
		if !c.acceptsPreviewType(previewType) {
			return false
		}
		if _, active := c.activePreviewIDs[previewID]; active {
			return false
		}
		c.activePreviewIDs[previewID] = previewType
		return true
	}
	activeType, active := c.activePreviewIDs[previewID]
	return active && activeType != "agent.thinking"
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
	if h.isFixtureThread(r, sessionID, threadID) {
		h.streamEvents(w, r, sessionID, threadID, false)
		return
	}
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
	_, ok := w.(http.Flusher)
	if !ok {
		h.errorAdapter.Write(w, r, streamingUnsupported())
		return
	}
	subID, ch := h.streams.subscribe(session.WorkspaceUUID, sessionID)
	defer h.streams.unsubscribe(subID)
	if err := h.eventBus.Subscribe(r.Context(), session.ExternalID); err != nil {
		h.logger.WarnContext(r.Context(), "session stream using database polling", "session_id", session.ExternalID, "error", err)
	} else {
		defer func() {
			if err := h.eventBus.Unsubscribe(session.ExternalID); err != nil {
				h.logger.WarnContext(r.Context(), "unsubscribe session event stream", "session_id", session.ExternalID, "error", err)
			}
		}()
	}
	progressCtx, cancelProgress := context.WithTimeout(r.Context(), 10*time.Second)
	cursor, err := h.db.SessionEventWatermark(progressCtx, session.WorkspaceUUID, sessionID)
	cancelProgress()
	if err != nil {
		h.errorAdapter.Write(w, r, internalError("Could not read event progress", err))
		return
	}
	connection := newStreamConnection(subscribeThreadID, primaryThread, streamDeltaTypes)
	connection.historyThreadID = threadID
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if err := writeStreamComment(w, "connected"); err != nil {
		return
	}
	h.followSessionEvents(w, r, session.WorkspaceUUID, sessionID, connection, ch, cursor)
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

func writeSSE(w http.ResponseWriter, event sessionStreamEvent, threadID string) error {
	payload := event.Payload
	if !maevents.IsStreamDelta(event.EventType) {
		if event.ThreadExternalID != nil {
			threadID = *event.ThreadExternalID
		}
		payload = eventPayloadForResponse(payload, event.CreatedAt, event.ProcessedAt, threadID)
	} else if event.EventType == previewEventStart {
		if eventType, eventID := streamPreviewTarget(event); eventType == "agent.thinking" {
			payload = eventStartPayload(previewBlock{eventID: eventID, eventType: eventType})
		}
	}
	if err := setStreamWriteDeadline(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.EventType, payload); err != nil {
		return err
	}
	return flushSessionStream(w)
}

func flushSessionStream(w http.ResponseWriter) error {
	controller := http.NewResponseController(w)
	err := controller.Flush()
	clearErr := controller.SetWriteDeadline(time.Time{})
	if errors.Is(clearErr, http.ErrNotSupported) {
		clearErr = nil
	}
	return errors.Join(err, clearErr)
}

func setStreamWriteDeadline(w http.ResponseWriter) error {
	err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func writeStreamComment(w http.ResponseWriter, comment string) error {
	if err := setStreamWriteDeadline(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, ": %s\n\n", comment); err != nil {
		return err
	}
	return flushSessionStream(w)
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
