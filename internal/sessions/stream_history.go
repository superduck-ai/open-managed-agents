package sessions

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

// Notifications only wake this reader. Reading committed rows in order also
// repairs a lost last notification, without a durable message bus.
func (h *Handler) followSessionEvents(w http.ResponseWriter, r *http.Request, workspaceUUID, sessionID string, connection *streamConnection, deliveries <-chan streamDelivery, cursor time.Time) {
	poll := time.NewTicker(time.Second)
	defer poll.Stop()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	deleted, err := h.catchUpSessionEvents(w, r.Context(), workspaceUUID, sessionID, connection, &cursor)
	if err != nil || deleted {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
		case <-keepalive.C:
			if err := writeStreamComment(w, "keepalive"); err != nil {
				return
			}
		case delivery, ok := <-deliveries:
			if !ok {
				return
			}
			if incoming, ok := delivery.(sessionEventDelivery); !ok || maevents.IsStreamDelta(incoming.event.EventType) {
				if ok && incoming.event.EventType == previewEventStart {
					deleted, err := h.catchUpSessionEvents(w, r.Context(), workspaceUUID, sessionID, connection, &cursor)
					if err != nil || deleted {
						return
					}
				}
				if err := h.writeSessionPreview(w, r.Context(), connection, delivery); err != nil {
					return
				}
				continue
			}
			// Complete deliveries are wakeups; they never bypass the DB cursor.
		}
		deleted, err := h.catchUpSessionEvents(w, r.Context(), workspaceUUID, sessionID, connection, &cursor)
		if err != nil || deleted {
			return
		}
	}
}

func (h *Handler) sessionPreviewTerminated(ctx context.Context, workspaceUUID, sessionID, threadID string) (bool, error) {
	session, found, err := h.db.GetSession(ctx, workspaceUUID, sessionID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, db.ErrNotFound
	}
	if session.Status == "terminated" {
		return true, nil
	}
	thread, err := h.db.GetSessionThread(ctx, workspaceUUID, sessionID, threadID)
	return thread.Status == "terminated", err
}

func (h *Handler) writeSessionPreview(w http.ResponseWriter, ctx context.Context, connection *streamConnection, delivery streamDelivery) error {
	if incoming, ok := delivery.(sessionEventDelivery); ok && incoming.event.EventType == previewEventStart {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, id := streamPreviewTarget(incoming.event)
		// A delayed start must not reopen a completed message, including finals
		// before this live-only connection's starting cursor.
		_, err := h.db.GetSessionEvent(ctx, incoming.event.WorkspaceUUID, incoming.event.SessionExternalID, id)
		if err == nil {
			return nil
		}
		if !errors.Is(err, db.ErrNotFound) {
			return err
		}
	}
	event, accepted := connection.event(delivery)
	if !accepted {
		return nil
	}
	return writeSSE(w, event, connection.threadID)
}

func (h *Handler) catchUpSessionEvents(w http.ResponseWriter, ctx context.Context, workspaceUUID, sessionID string, connection *streamConnection, cursor *time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	watermark, err := h.db.SessionEventWatermark(ctx, workspaceUUID, sessionID)
	if err != nil {
		return false, err
	}
	if !watermark.After(*cursor) {
		return false, nil
	}
	var page *db.SessionEventPageCursor
	for {
		// ponytail: scan the Session log so cross-posted terminal controls reach
		// every connection; use a scoped control query if fanout volume warrants it.
		records, more, err := h.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
			WorkspaceUUID: workspaceUUID, SessionExternalID: sessionID,
			IncludeDeleted: true,
			Limit:          256, Order: "asc", Cursor: page, CreatedAtGT: cursor, CreatedAtLTE: &watermark,
		})
		if err != nil {
			return false, err
		}
		for _, record := range records {
			if !maevents.IsPublicSessionHistoryEvent(record.EventType) {
				continue
			}
			event := sessionStreamEventFrom(record)
			if connection.accepts(event) {
				if err := writeSSE(w, event, connection.threadID); err != nil {
					return false, err
				}
			}
			if event.EventType == "session.deleted" {
				return true, nil
			}
		}
		if !more {
			*cursor = watermark
			return false, nil
		}
		last := records[len(records)-1]
		page = &db.SessionEventPageCursor{ProcessedAt: last.ProcessedAt, ExternalID: last.ExternalID}
	}
}
