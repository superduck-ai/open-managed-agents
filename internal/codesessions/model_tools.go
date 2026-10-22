package codesessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) modelToolUsePayloads(ctx context.Context, request *ModelRequest, tools []ModelRequestToolUse, at time.Time) ([]json.RawMessage, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	external, err := s.selfHostedCodeSession(ctx, request.CodeSessionID)
	if err != nil {
		return nil, err
	}
	var payloads []json.RawMessage
	for _, tool := range tools {
		if request.toolThreadID != "" && isDelegationTool(tool.Name) {
			continue
		}
		permission, identity, err := s.resolveToolPermission(ctx, request.CodeSessionID, tool.Name, request.toolThreadID)
		if err != nil {
			return nil, err
		}
		event, err := modelToolUsePayload(request, tool, identity, permission, external, at)
		if err != nil {
			return nil, err
		}
		if len(event) > 0 {
			payloads = append(payloads, event)
		}
	}
	return payloads, nil
}
func modelToolUsePayload(request *ModelRequest, tool ModelRequestToolUse, identity toolIdentity, permission resolvedToolPermission, external bool, at time.Time) (json.RawMessage, error) {
	if identity.Kind == "agent_toolset" && identity.ToolName == "ask_user_question" {
		return nil, nil
	}
	if tool.InputRejected {
		eventType, _ := toolPermissionPublicIdentity(tool.Name, identity)
		if identity.Kind != "agent_toolset" || eventType != "agent.tool_use" {
			return nil, nil
		}
		permission = resolvedToolPermissionDeny
	} else if permission != resolvedToolPermissionAllow || (external && identity.Kind == "agent_toolset") {
		return nil, nil
	}
	var input map[string]any
	if err := json.Unmarshal(tool.Input, &input); err != nil {
		return nil, err
	}
	call := toolPermissionRequest{ToolName: tool.Name, ToolUseID: tool.ID, Input: input, SessionThreadID: request.toolThreadID}
	_, event, err := toolCallPublicPayload(request.CodeSessionID, call, identity, permission, at)
	return event, err
}

func (s *Service) modelToolTurnPending(ctx context.Context, record db.CodeSession) (bool, error) {
	events, _, err := s.eventPayloads.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceUUID: record.WorkspaceUUID, SessionExternalID: record.SessionExternalID,
		PrimaryOnly: true, Order: "desc", Limit: 1,
		Types: []string{"span.model_request_end", "user.interrupt", "session.error"},
	})
	if err != nil || len(events) == 0 || events[0].EventType != "span.model_request_end" {
		return false, err
	}
	var end modelRequestEvent
	if err := json.Unmarshal(events[0].Payload, &end); err != nil {
		return false, err
	}
	return end.Error == nil && len(end.ToolUseIDs) > 0, nil
}
