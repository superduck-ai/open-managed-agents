package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func sessionInputRequest(key string, events []byte) (*db.SessionInputRequest, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	if len(key) > 128 {
		return nil, errors.New("Idempotency-Key must contain at most 128 bytes")
	}
	canonical := jsontext.Value(events).Clone()
	if err := canonical.Canonicalize(jsontext.CanonicalizeRawInts(false), jsontext.CanonicalizeRawFloats(false)); err != nil {
		return nil, err
	}
	return new(db.SessionInputRequest{Key: key, Fingerprint: fmt.Sprintf("%x", sha256.Sum256(canonical))}), nil
}

func (h *Handler) deliverSessionInputChanges(w http.ResponseWriter, r *http.Request, session db.Session, changes db.SessionEventChanges, request *db.SessionInputRequest) error {
	if !changes.Replayed {
		h.codeSessions.PurgeWorkerEvents(r.Context(), changes.RetiredCodeSessionIDs)
		recordAcceptedSessionEvents(r.Context(), changes.Events)
		h.publishSessionEvents(r.Context(), changes.Events)
		h.enqueueWebhooksForSessionEvents(r.Context(), session.WorkspaceUUID, session.ExternalID, changes.Events)
	}
	if !changes.InputHandoffCompleted {
		if err := queueSessionInputChanges(r.Context(), session, changes, h.codeSessions.QueuePublicSessionEvents); err != nil {
			h.logger.ErrorContext(r.Context(), "queue session events for code session", "session_id", session.ExternalID, "error", err)
			recordSessionHandoffFailure(r.Context(), err)
			return queueCodeSessionEventsError(err)
		}
		if request != nil {
			if err := h.db.CompleteSessionInputRequestHandoff(r.Context(), session, *request); err != nil {
				return mapSessionLoadError(err, session.ExternalID)
			}
		}
	}
	data := make([]json.RawMessage, 0, len(changes.Events))
	for _, event := range changes.Events {
		if allowedPublicEventType(event.EventType) {
			data = append(data, sessionEventPayload(event))
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, sendEventsResponse{Data: data})
	return nil
}

func queueSessionInputChanges(ctx context.Context, session db.Session, changes db.SessionEventChanges, enqueue func(context.Context, db.Session, []db.SessionEvent) error) error {
	if !changes.Replayed {
		return enqueue(ctx, session, changes.WorkerInputEvents())
	}
	for _, event := range changes.WorkerInputEvents() {
		err := enqueue(ctx, session, []db.SessionEvent{event})
		control := event.EventType == "user.tool_confirmation" || event.EventType == "user.custom_tool_result" || event.EventType == "user.tool_result"
		if control && (errors.Is(err, db.ErrSessionToolUseNotPending) || errors.Is(err, db.ErrSessionInputConflict)) {
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}
