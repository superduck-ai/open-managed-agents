package sessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
)

type sessionEventsFanout struct {
	Events []sessionStreamEvent `json:"events"`
}

type sessionStreamEvent struct {
	ExternalID        string          `json:"external_id"`
	WorkspaceUUID     string          `json:"workspace_uuid"`
	SessionExternalID string          `json:"session_id"`
	ThreadExternalID  *string         `json:"thread_id,omitempty"`
	PrimaryThread     bool            `json:"primary_thread,omitempty"`
	EventType         string          `json:"event_type"`
	Payload           json.RawMessage `json:"payload"`
	ProcessedAt       time.Time       `json:"processed_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at,omitempty"`
}

type codeSessionStreamFanout struct {
	WorkspaceUUID     string            `json:"workspace_uuid"`
	SessionExternalID string            `json:"session_id"`
	CodeSessionID     string            `json:"code_session_id"`
	WorkerEpoch       int64             `json:"worker_epoch"`
	Payloads          []json.RawMessage `json:"payloads"`
}

func (h *Handler) publishSessionEvents(ctx context.Context, events []db.SessionEvent) {
	if len(events) == 0 {
		return
	}
	payloads := make(map[string]*sessionEventsFanout)
	sessionIDs := make([]string, 0, 1)
	for _, event := range events {
		payload, exists := payloads[event.SessionExternalID]
		if !exists {
			payload = &sessionEventsFanout{}
			payloads[event.SessionExternalID] = payload
			sessionIDs = append(sessionIDs, event.SessionExternalID)
		}
		payload.Events = append(payload.Events, sessionStreamEventFrom(event))
	}
	for _, sessionID := range sessionIDs {
		payload := payloads[sessionID]
		if err := h.publishFanout(ctx, sessionID, sessionfanout.KindSessionEvents, payload); err != nil {
			h.logger.WarnContext(ctx, "publish session events fanout", "session_id", sessionID, "event_count", len(payload.Events), "error", err)
		}
	}
}

func (h *Handler) PublishCodeSessionStreamEvents(ctx context.Context, route codesessions.CodeSessionStreamRoute, workerEpoch int64, payloads []json.RawMessage) error {
	if len(payloads) == 0 {
		return nil
	}
	payload := codeSessionStreamFanout{
		WorkspaceUUID:     route.WorkspaceUUID,
		SessionExternalID: route.SessionExternalID,
		CodeSessionID:     route.CodeSessionID,
		WorkerEpoch:       workerEpoch,
		Payloads:          payloads,
	}
	if err := h.publishFanout(ctx, route.SessionExternalID, sessionfanout.KindCodeSessionStream, payload); err != nil {
		h.logger.WarnContext(ctx, "publish code session stream fanout", "code_session_id", route.CodeSessionID, "worker_epoch", workerEpoch, "event_count", len(payloads), "error", err)
	}
	return nil
}

func (h *Handler) publishFanout(ctx context.Context, sessionID string, kind sessionfanout.Kind, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return h.eventBus.Publish(ctx, sessionID, sessionfanout.Envelope{
		Kind:    kind,
		Payload: raw,
	})
}

func (h *Handler) receiveFanout(ctx context.Context, envelope sessionfanout.Envelope) {
	switch envelope.Kind {
	case sessionfanout.KindSessionEvents:
		var payload sessionEventsFanout
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			h.logger.WarnContext(ctx, "discard session events fanout", "error", err)
			return
		}
		h.receiveSessionEventsFanout(ctx, payload.Events)
	case sessionfanout.KindCodeSessionStream:
		var payload codeSessionStreamFanout
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			h.logger.WarnContext(ctx, "discard code session stream fanout", "error", err)
			return
		}
		for _, event := range h.previews.convert(payload) {
			h.streams.broadcastEvent(event)
		}
	}
}

func (h *Handler) receiveSessionEventsFanout(ctx context.Context, events []sessionStreamEvent) {
	terminalSessionID := ""
	for _, event := range events {
		h.streams.broadcastEvent(event)
		if status, ok := maevents.SessionStatus(event.EventType); ok && status == "terminated" {
			terminalSessionID = event.SessionExternalID
		}
	}
	if terminalSessionID == "" {
		return
	}
	if err := h.eventBus.Unsubscribe(ctx, terminalSessionID); err != nil {
		h.logger.WarnContext(ctx, "unsubscribe terminated session fanout", "session_id", terminalSessionID, "error", err)
	}
}

func (h *Handler) resetFanout() {
	h.previews.reset()
	h.streams.reset()
}

func sessionStreamEventFrom(event db.SessionEvent) sessionStreamEvent {
	return sessionStreamEvent{
		ExternalID:        event.ExternalID,
		WorkspaceUUID:     event.WorkspaceUUID,
		SessionExternalID: event.SessionExternalID,
		ThreadExternalID:  event.ThreadExternalID,
		EventType:         event.EventType,
		Payload:           event.Payload,
		ProcessedAt:       event.ProcessedAt,
		CreatedAt:         event.CreatedAt,
	}
}
