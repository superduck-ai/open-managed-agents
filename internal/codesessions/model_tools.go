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
	session, found, err := s.db.GetSession(ctx, request.codeSession.WorkspaceUUID, request.codeSession.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	var payloads []json.RawMessage
	for _, tool := range tools {
		permission, identity := resolveToolPermissionFromAgentSnapshot(session.AgentSnapshot, tool.Name)
		if permission != resolvedToolPermissionAllow {
			continue
		}
		var input map[string]any
		if err := json.Unmarshal(tool.Input, &input); err != nil {
			return nil, err
		}
		call := toolPermissionRequest{
			ToolName: tool.Name, ToolUseID: tool.ID, Input: input, SessionThreadID: request.toolThreadID,
		}
		_, event, err := toolCallPublicPayload(request.CodeSessionID, call, identity, permission, at)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, event)
	}
	return payloads, nil
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
