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
	WorkspaceUUID     string          `json:"workspace_uuid"`
	SessionExternalID string          `json:"session_id"`
	CodeSessionID     string          `json:"code_session_id"`
	WorkerEpoch       int64           `json:"worker_epoch"`
	Payload           json.RawMessage `json:"payload"`
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

func (h *Handler) PublishCodeSessionStreamEvent(ctx context.Context, route codesessions.CodeSessionStreamRoute, workerEpoch int64, streamPayload json.RawMessage) error {
	if len(streamPayload) == 0 {
		return nil
	}
	payload := codeSessionStreamFanout{
		WorkspaceUUID:     route.WorkspaceUUID,
		SessionExternalID: route.SessionExternalID,
		CodeSessionID:     route.CodeSessionID,
		WorkerEpoch:       workerEpoch,
		Payload:           streamPayload,
	}
	return h.publishFanout(ctx, route.SessionExternalID, sessionfanout.KindCodeSessionStream, payload)
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

func (h *Handler) receiveFanout(ctx context.Context, _ string, envelope sessionfanout.Envelope) {
	switch envelope.Kind {
	case sessionfanout.KindSessionEvents:
		var payload sessionEventsFanout
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			h.logger.WarnContext(ctx, "discard session events fanout", "error", err)
			return
		}
		h.receiveSessionEventsFanout(payload.Events)
	case sessionfanout.KindCodeSessionStream:
		var payload codeSessionStreamFanout
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			h.logger.WarnContext(ctx, "discard code session stream fanout", "error", err)
			return
		}
		if event, emit := h.previews.convert(payload); emit {
			h.streams.broadcastEvent(event)
		}
	}
}

func (h *Handler) receiveSessionEventsFanout(events []sessionStreamEvent) {
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
	h.previews.resetSession(terminalSessionID)
}

func (h *Handler) resetFanout(_ context.Context, sessionID string) {
	h.previews.resetSession(sessionID)
	h.streams.resetSession(sessionID)
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
