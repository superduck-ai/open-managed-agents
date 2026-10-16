package sessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (h *Handler) threadMessageCopySpecs(ctx context.Context, session db.Session, codeSessionID, eventType, eventID string, payload map[string]any) ([]sessionEventCopySpec, error) {
	primaryOnly := []sessionEventCopySpec{newSessionEventCopySpec(eventID, payload, nil, false)}
	field := "to_session_thread_id"
	if eventType == "agent.thread_message_received" {
		field = "from_session_thread_id"
	}
	childID := sessionPayloadString(payload, field)
	if childID == "" {
		return primaryOnly, nil
	}
	child, err := h.db.GetSessionThread(ctx, session.WorkspaceUUID, session.ExternalID, childID)
	if err != nil {
		return nil, err
	}
	primary, err := h.ensurePrimarySessionThread(ctx, session)
	if err != nil {
		return nil, err
	}
	if child.ParentThreadExternalID == nil || *child.ParentThreadExternalID != primary.ExternalID {
		return primaryOnly, nil
	}
	var agent struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(primary.AgentSnapshot, &agent); err != nil {
		return nil, err
	}
	childCopy := childThreadMessageCopy(codeSessionID, eventType, eventID, payload, childID, primary.ExternalID, agent.Name)
	return append(primaryOnly, childCopy), nil
}

func childThreadMessageCopy(codeSessionID, eventType, eventID string, payload map[string]any, childID, primaryID, primaryName string) sessionEventCopySpec {
	child := copySessionEventPayload(payload)
	delete(child, "to_session_thread_id")
	delete(child, "to_agent_name")
	delete(child, "from_session_thread_id")
	delete(child, "from_agent_name")
	delete(child, "agent_name")
	child["session_thread_id"] = childID
	child["original_event_id"] = eventID
	if eventType == "agent.thread_message_sent" {
		child["type"] = "agent.thread_message_received"
		child["from_session_thread_id"] = primaryID
		if primaryName != "" {
			child["from_agent_name"] = primaryName
		}
	} else {
		child["type"] = "agent.thread_message_sent"
		child["to_session_thread_id"] = primaryID
		if primaryName != "" {
			child["to_agent_name"] = primaryName
		}
	}
	id := derivedSessionEventID(codeSessionID, eventID, sessionPayloadString(child, "type"), childID)
	return newSessionEventCopySpec(id, child, &childID, true)
}
