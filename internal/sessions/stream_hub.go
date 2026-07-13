package sessions

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/go-chi/chi/v5"
)

type streamHub struct {
	mu          sync.Mutex
	nextSubID   int64
	subscribers map[int64]subscriber
}

type subscriber struct {
	sessionID           string
	threadID            string
	includeStreamDeltas bool
	ch                  chan db.SessionEvent
}

func newStreamHub() *streamHub {
	return &streamHub{subscribers: map[int64]subscriber{}}
}

func (h *Handler) subscribe(sessionID, threadID string, includeStreamDeltas bool) (int64, <-chan db.SessionEvent) {
	return h.streams.subscribe(sessionID, threadID, includeStreamDeltas)
}

func (h *Handler) unsubscribe(id int64) {
	h.streams.unsubscribe(id)
}

func (h *Handler) broadcast(event db.SessionEvent) {
	h.streams.broadcast(event)
}

func (h *Handler) broadcastStreamDelta(event db.SessionEvent) {
	h.streams.broadcastStreamDelta(event)
}

func (h *streamHub) subscribe(sessionID, threadID string, includeStreamDeltas bool) (int64, <-chan db.SessionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSubID++
	id := h.nextSubID
	ch := make(chan db.SessionEvent, 32)
	h.subscribers[id] = subscriber{sessionID: sessionID, threadID: threadID, includeStreamDeltas: includeStreamDeltas, ch: ch}
	return id, ch
}

func (h *streamHub) unsubscribe(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subscribers, id)
}

func (h *streamHub) broadcast(event db.SessionEvent) {
	if !maevents.IsPublicSessionHistoryEvent(event.EventType) {
		return
	}
	h.broadcastToSubscribers(event, false)
}

func (h *streamHub) broadcastStreamDelta(event db.SessionEvent) {
	if !maevents.IsStreamDelta(event.EventType) {
		return
	}
	h.broadcastToSubscribers(event, true)
}

func (h *streamHub) broadcastToSubscribers(event db.SessionEvent, streamDelta bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		if streamDelta && !sub.includeStreamDeltas {
			continue
		}
		if sub.sessionID != event.SessionExternalID {
			continue
		}
		if sub.threadID == "" && event.ThreadExternalID != nil {
			continue
		}
		if sub.threadID != "" && (event.ThreadExternalID == nil || *event.ThreadExternalID != sub.threadID) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
		}
	}
}

func (h *Handler) streamEventsRoute(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, chi.URLParam(r, "session_id"), "")
}

func (h *Handler) StreamEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	h.streamEvents(w, r, sessionID, "")
}

func (h *Handler) streamThreadEventsRoute(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	threadID := chi.URLParam(r, "thread_id")
	if h.isFixtureThread(r, sessionID, threadID) {
		h.streamEvents(w, r, sessionID, threadID)
		return
	}
	if _, ok := h.authorizeSession(w, r, sessionID, sessionAccessEventsRead); !ok {
		return
	}
	if _, err := h.db.GetSessionThread(r.Context(), workspaceIDFromRequest(r), sessionID, threadID); err != nil {
		writeThreadLoadError(w, r, err, threadID)
		return
	}
	h.streamEvents(w, r, sessionID, threadID)
}

func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request, sessionID, threadID string) {
	session, ok := h.authorizeSession(w, r, sessionID, sessionAccessEventsRead)
	if !ok {
		return
	}
	subscribeThreadID := threadID
	if subscribeThreadID == "" {
		primary, err := h.ensurePrimarySessionThread(r.Context(), session)
		if err != nil {
			writeSessionLoadError(w, r, err, sessionID)
			return
		}
		subscribeThreadID = primary.ExternalID
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Streaming is not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	subID, ch := h.subscribe(sessionID, subscribeThreadID, acceptsStreamDeltas(r))
	defer h.unsubscribe(subID)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case event := <-ch:
			writeSSE(w, event, threadID)
			flusher.Flush()
		}
	}
}

func acceptsStreamDeltas(r *http.Request) bool {
	return len(parseRepeatedQuery(r, "event_deltas[]", "event_deltas")) > 0
}

func writeSSE(w http.ResponseWriter, event db.SessionEvent, threadID string) {
	fmt.Fprintf(w, "event: %s\n", event.EventType)
	fmt.Fprintf(w, "data: %s\n\n", sessionEventPayloadForResponse(event, threadID))
}
