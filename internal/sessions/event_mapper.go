package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/google/uuid"
)

func rawSessionEventType(raw json.RawMessage) string {
	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Type)
}

func (h *Handler) streamDeltaEventFromCodeSessionPayload(ctx context.Context, session db.Session, codeSessionID string, raw json.RawMessage, now time.Time) (db.SessionEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return db.SessionEvent{}, errors.New("code session stream delta must be an object")
	}
	eventType := strings.TrimSpace(sessionPayloadString(payload, "type"))
	if !maevents.IsStreamDelta(eventType) {
		return db.SessionEvent{}, errors.New("code session stream delta type is required")
	}
	eventID := firstSessionPayloadString(payload, "id", "uuid")
	if eventID == "" {
		eventID = stableCodeSessionEventID(codeSessionID, raw)
	}
	payload["id"] = eventID
	if sessionPayloadString(payload, "uuid") == "" {
		payload["uuid"] = eventID
	}
	createdAt := now
	if rawCreatedAt := firstSessionPayloadString(payload, "created_at", "timestamp"); rawCreatedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, rawCreatedAt); err == nil {
			createdAt = parsed.UTC()
		}
	}
	if sessionPayloadString(payload, "created_at") == "" {
		payload["created_at"] = httpapi.FormatTime(createdAt)
	}
	threadID := streamDeltaOwnerThreadID(payload)
	delete(payload, "owner_session_thread_id")
	delete(payload, "_owner_session_thread_id")
	if threadID == "" {
		primary, err := h.db.GetPrimarySessionThread(ctx, session.WorkspaceID, session.ExternalID)
		if err != nil {
			return db.SessionEvent{}, err
		}
		threadID = primary.ExternalID
	}
	payloadRaw, err := httpapi.MarshalRaw(payload)
	if err != nil {
		return db.SessionEvent{}, err
	}
	return db.SessionEvent{
		UUID:              uuid.NewString(),
		ExternalID:        eventID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionID:         session.ID,
		SessionExternalID: session.ExternalID,
		ThreadExternalID:  &threadID,
		EventType:         eventType,
		Payload:           payloadRaw,
		CreatedAt:         createdAt,
	}, nil
}

func streamDeltaOwnerThreadID(payload map[string]any) string {
	if threadID := firstSessionPayloadString(payload, "owner_session_thread_id", "_owner_session_thread_id", "session_thread_id", "thread_id"); threadID != "" {
		return threadID
	}
	if nested, ok := payload["event"].(map[string]any); ok {
		return firstSessionPayloadString(nested, "owner_session_thread_id", "_owner_session_thread_id", "session_thread_id", "thread_id")
	}
	return ""
}

type sessionEventCopySpec struct {
	OwnerThreadID *string
	Payload       map[string]any
	EventID       string
}

func (h *Handler) sessionEventsFromCodeSessionPayload(ctx context.Context, session db.Session, codeSessionID string, raw json.RawMessage, now time.Time) ([]db.SessionEvent, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("code session event must be an object")
	}
	eventType, _ := payload["type"].(string)
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return nil, errors.New("code session event type is required")
	}
	if maevents.IsStreamDelta(eventType) {
		return nil, errors.New("stream delta events are not persisted")
	}
	eventID, _ := payload["id"].(string)
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		eventID = stableCodeSessionEventID(codeSessionID, raw)
		payload["id"] = eventID
	}
	processedAt := now
	if rawProcessedAt, ok := payload["processed_at"].(string); ok && strings.TrimSpace(rawProcessedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(rawProcessedAt)); err == nil {
			processedAt = parsed.UTC()
		}
	}
	payload["processed_at"] = httpapi.FormatTime(processedAt)
	createdAt := processedAt
	if rawCreatedAt, ok := payload["created_at"].(string); ok && strings.TrimSpace(rawCreatedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(rawCreatedAt)); err == nil {
			createdAt = parsed.UTC()
		}
	}
	payload["created_at"] = httpapi.FormatTime(createdAt)
	if err := h.populateThreadCoordinationAgentNames(ctx, session, eventType, payload); err != nil {
		return nil, err
	}
	specs, err := h.sessionEventCopySpecs(ctx, session, codeSessionID, eventType, eventID, payload, createdAt)
	if err != nil {
		return nil, err
	}
	events := make([]db.SessionEvent, 0, len(specs))
	for _, spec := range specs {
		payloadRaw, err := httpapi.MarshalRaw(spec.Payload)
		if err != nil {
			return nil, err
		}
		events = append(events, db.SessionEvent{
			UUID:              uuid.NewString(),
			ExternalID:        spec.EventID,
			OrganizationID:    session.OrganizationID,
			WorkspaceID:       session.WorkspaceID,
			SessionID:         session.ID,
			SessionExternalID: session.ExternalID,
			ThreadExternalID:  spec.OwnerThreadID,
			EventType:         eventType,
			Payload:           payloadRaw,
			ProcessedAt:       processedAt,
			CreatedAt:         createdAt,
		})
	}
	return events, nil
}

func (h *Handler) populateThreadCoordinationAgentNames(ctx context.Context, session db.Session, eventType string, payload map[string]any) error {
	switch eventType {
	case "agent.thread_message_received":
		if sessionPayloadString(payload, "from_agent_name") != "" {
			return nil
		}
		name, err := h.agentNameForSessionThread(ctx, session, sessionPayloadString(payload, "from_session_thread_id"))
		if err != nil {
			return err
		}
		if name != "" {
			payload["from_agent_name"] = name
		}
	case "agent.thread_message_sent":
		if sessionPayloadString(payload, "to_agent_name") != "" {
			return nil
		}
		name, err := h.agentNameForSessionThread(ctx, session, sessionPayloadString(payload, "to_session_thread_id"))
		if err != nil {
			return err
		}
		if name != "" {
			payload["to_agent_name"] = name
		}
	}
	return nil
}

func (h *Handler) agentNameForSessionThread(ctx context.Context, session db.Session, threadID string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	thread, err := h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, threadID)
	if errors.Is(err, db.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	snapshot, ok := agentsnapshot.RawJSONValue(thread.AgentSnapshot, nil).(map[string]any)
	if !ok {
		return "", nil
	}
	return firstSessionPayloadString(snapshot, "name", "display_name"), nil
}

func shouldInferOwnerThreadFromPayload(eventType string) bool {
	switch maevents.CategoryFor(eventType) {
	case maevents.CategoryAgent, maevents.CategoryTool, maevents.CategorySpan:
		return true
	default:
		return false
	}
}

func (h *Handler) inferOwnerSessionThreadID(ctx context.Context, session db.Session, payload map[string]any) (string, error) {
	candidates := make(map[string]struct{})
	for _, field := range []string{"agent_id", "agentId", "task_id"} {
		if value := sessionPayloadString(payload, field); value != "" {
			candidates[value] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	events, _, err := h.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceID:       session.WorkspaceID,
		SessionExternalID: session.ExternalID,
		PrimaryOnly:       true,
		Limit:             500,
		Order:             "asc",
		Types:             []string{"session.thread_created"},
	})
	if err != nil {
		return "", err
	}
	for _, event := range events {
		var object map[string]any
		if err := json.Unmarshal(event.Payload, &object); err != nil {
			continue
		}
		threadID := sessionPayloadString(object, "session_thread_id")
		if threadID == "" {
			continue
		}
		for _, field := range []string{"agent_id", "agentId", "task_id"} {
			if value := sessionPayloadString(object, field); value != "" {
				if _, ok := candidates[value]; ok {
					// 子线程内部事件有时只带 agent/task 标识。这里把它归还给对应
					// session thread，避免普通工具调用被误写到 primary 线程。
					return threadID, nil
				}
			}
		}
	}
	return "", nil
}

func (h *Handler) sessionEventCopySpecs(ctx context.Context, session db.Session, codeSessionID, eventType, eventID string, payload map[string]any, now time.Time) ([]sessionEventCopySpec, error) {
	ownerThreadID := sessionPayloadString(payload, "owner_session_thread_id")
	if ownerThreadID == "" {
		ownerThreadID = sessionPayloadString(payload, "_owner_session_thread_id")
	}
	delete(payload, "owner_session_thread_id")
	delete(payload, "_owner_session_thread_id")
	if ownerThreadID == "" && shouldInferOwnerThreadFromPayload(eventType) {
		inferredThreadID, err := h.inferOwnerSessionThreadID(ctx, session, payload)
		if err != nil {
			return nil, err
		}
		ownerThreadID = inferredThreadID
	}
	if ownerThreadID != "" {
		if err := h.ensureSessionThread(ctx, session, ownerThreadID, payload, now); err != nil {
			return nil, err
		}
		if h.shouldCrossPostBlockingToolEvent(eventType, payload, ownerThreadID) {
			return h.dualSessionEventCopySpecs(codeSessionID, eventType, eventID, payload, ownerThreadID, true), nil
		}
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, &ownerThreadID, false)}, nil
	}
	threadID := sessionPayloadString(payload, "session_thread_id")
	if eventType == "session.thread_created" {
		if threadID != "" {
			if err := h.ensureSessionThread(ctx, session, threadID, payload, now); err != nil {
				return nil, err
			}
		}
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
	}
	if _, ok := maevents.ThreadStatus(eventType); ok && threadID != "" {
		if err := h.ensureSessionThread(ctx, session, threadID, payload, now); err != nil {
			return nil, err
		}
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
	}
	if h.shouldCrossPostBlockingToolEvent(eventType, payload, threadID) {
		if err := h.ensureSessionThread(ctx, session, threadID, payload, now); err != nil {
			return nil, err
		}
		return h.dualSessionEventCopySpecs(codeSessionID, eventType, eventID, payload, threadID, true), nil
	}
	if maevents.IsPrimaryCoordinationEvent(eventType) {
		return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
	}
	if threadID != "" {
		if _, err := h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, threadID); err == nil {
			ownerPayload := copySessionEventPayload(payload)
			if sessionToolUseEventHasThreadScopedSessionThreadID(eventType) {
				delete(ownerPayload, "session_thread_id")
			}
			return []sessionEventCopySpec{newSessionEventCopySpec(eventID, ownerPayload, &threadID, false)}, nil
		}
	}
	return []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}, nil
}

func (h *Handler) dualSessionEventCopySpecs(codeSessionID, eventType, eventID string, payload map[string]any, threadID string, stripThreadIDFromThreadCopy bool) []sessionEventCopySpec {
	threadPayload := copySessionEventPayload(payload)
	if stripThreadIDFromThreadCopy {
		delete(threadPayload, "session_thread_id")
	}
	primaryPayload := copySessionEventPayload(payload)
	primaryPayload["session_thread_id"] = threadID
	primaryEventID := derivedSessionEventID(codeSessionID, eventID, eventType, "primary")
	return []sessionEventCopySpec{
		newSessionEventCopySpec(eventID, threadPayload, &threadID, false),
		newSessionEventCopySpec(primaryEventID, primaryPayload, nil, true),
	}
}

func newSessionEventCopySpec(eventID string, payload map[string]any, ownerThreadID *string, forceID bool) sessionEventCopySpec {
	if forceID {
		payload = copySessionEventPayload(payload)
		payload["id"] = eventID
	}
	return sessionEventCopySpec{OwnerThreadID: ownerThreadID, Payload: payload, EventID: eventID}
}

func derivedSessionEventID(codeSessionID, eventID, eventType, stream string) string {
	sum := sha256.Sum256([]byte(codeSessionID + "\x00" + eventID + "\x00" + eventType + "\x00" + stream))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func copySessionEventPayload(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (h *Handler) shouldCrossPostBlockingToolEvent(eventType string, payload map[string]any, threadID string) bool {
	if strings.TrimSpace(threadID) == "" || !sessionToolUseEventHasThreadScopedSessionThreadID(eventType) {
		return false
	}
	if eventType == "agent.custom_tool_use" {
		return true
	}
	permission := strings.ToLower(firstSessionPayloadString(payload, "evaluated_permission", "permission", "permission_decision"))
	return permission == "ask" || permission == "always_ask" || permission == "requires_action"
}

func sessionToolUseEventHasThreadScopedSessionThreadID(eventType string) bool {
	switch eventType {
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		return true
	default:
		return false
	}
}

func firstSessionPayloadString(payload map[string]any, fields ...string) string {
	for _, field := range fields {
		if value := sessionPayloadString(payload, field); value != "" {
			return value
		}
	}
	return ""
}

func (h *Handler) ensurePrimarySessionThread(ctx context.Context, session db.Session) (db.SessionThread, error) {
	thread, err := h.db.GetPrimarySessionThread(ctx, session.WorkspaceID, session.ExternalID)
	if err == nil {
		return thread, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.SessionThread{}, err
	}
	threadID, err := ids.New("sthr_")
	if err != nil {
		return db.SessionThread{}, err
	}
	now := time.Now().UTC()
	return h.db.CreateSessionThreadIfAbsent(ctx, db.SessionThread{
		UUID:              uuid.NewString(),
		ExternalID:        threadID,
		OrganizationID:    session.OrganizationID,
		WorkspaceID:       session.WorkspaceID,
		SessionID:         session.ID,
		SessionExternalID: session.ExternalID,
		AgentSnapshot:     session.AgentSnapshot,
		Status:            threadStatusForSession(session.Status),
		Usage:             json.RawMessage(`{}`),
		Stats:             json.RawMessage(`{}`),
		CreatedAt:         now,
		UpdatedAt:         now,
	})
}

func threadStatusForSession(status string) string {
	switch strings.TrimSpace(status) {
	case "running", "rescheduling", "idle", "terminated":
		return strings.TrimSpace(status)
	default:
		return "idle"
	}
}

func (h *Handler) ensureSessionThread(ctx context.Context, session db.Session, threadID string, payload map[string]any, now time.Time) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	if _, err := h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, threadID); err == nil {
		return nil
	} else if !errors.Is(err, db.ErrNotFound) {
		return err
	}
	var parentThreadID *int64
	var parentThreadExternalID *string
	parentExternalID := sessionPayloadString(payload, "parent_session_thread_id")
	var parent db.SessionThread
	var err error
	if parentExternalID != "" {
		parent, err = h.db.GetSessionThread(ctx, session.WorkspaceID, session.ExternalID, parentExternalID)
	} else {
		parent, err = h.db.GetPrimarySessionThread(ctx, session.WorkspaceID, session.ExternalID)
	}
	if err == nil && parent.ExternalID != threadID {
		parentThreadID = &parent.ID
		value := parent.ExternalID
		parentThreadExternalID = &value
	} else if err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	agentSnapshot, err := sessionThreadAgentSnapshot(payload)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err = h.db.CreateSessionThreadIfAbsent(ctx, db.SessionThread{
		UUID:                   uuid.NewString(),
		ExternalID:             threadID,
		OrganizationID:         session.OrganizationID,
		WorkspaceID:            session.WorkspaceID,
		SessionID:              session.ID,
		SessionExternalID:      session.ExternalID,
		ParentThreadID:         parentThreadID,
		ParentThreadExternalID: parentThreadExternalID,
		AgentSnapshot:          agentSnapshot,
		Status:                 "idle",
		Usage:                  json.RawMessage(`{}`),
		Stats:                  json.RawMessage(`{}`),
		CreatedAt:              now,
		UpdatedAt:              now,
	})
	return err
}

func sessionThreadAgentSnapshot(payload map[string]any) (json.RawMessage, error) {
	if agent, ok := payload["agent"].(map[string]any); ok && agent != nil {
		return httpapi.MarshalRaw(agent)
	}
	agentName := sessionPayloadString(payload, "agent_name")
	if agentName == "" {
		return json.RawMessage(`{}`), nil
	}
	return httpapi.MarshalRaw(map[string]any{"name": agentName})
}

func sessionPayloadString(payload map[string]any, name string) string {
	value, ok := payload[name].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func stableCodeSessionEventID(codeSessionID string, raw json.RawMessage) string {
	sum := sha256.Sum256([]byte(codeSessionID + "\x00" + strings.TrimSpace(string(raw))))
	return "sevt_" + hex.EncodeToString(sum[:16])
}
