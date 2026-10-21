package codesessions

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type selfHostedToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type selfHostedToolResult struct {
	ToolUseID       string                  `json:"tool_use_id"`
	SessionThreadID string                  `json:"session_thread_id"`
	Content         []selfHostedToolContent `json:"content"`
	IsError         bool                    `json:"is_error"`
}

func (s *Service) selfHostedCodeSession(ctx context.Context, id string) (bool, error) {
	code, found, err := s.db.GetCodeSession(ctx, id)
	if err != nil {
		return false, err
	}
	if !found {
		return false, db.ErrNotFound
	}
	if code.EnvironmentUUID == "" {
		return false, nil
	}
	env, err := s.db.GetEnvironmentByUUID(ctx, code.WorkspaceUUID, code.EnvironmentUUID)
	if err != nil {
		return false, err
	}
	var config struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Config, &config); err != nil {
		return false, err
	}
	return config.Type == "self_hosted", nil
}
func (s *Service) queueControlResponseForSelfHostedToolResult(ctx context.Context, code db.CodeSession, event db.SessionEvent) (bool, error) {
	external, err := s.selfHostedCodeSession(ctx, code.ExternalID)
	if err != nil || !external {
		return false, err
	}
	var result selfHostedToolResult
	if err := json.Unmarshal(event.Payload, &result); err != nil {
		return false, err
	}
	request, result, err := selfHostedPendingResult(code.WorkerExternalMetadata, code.ExternalID, result)
	if err != nil {
		return false, selfHostedResultRoutingError("pending_call", err)
	}
	if request.SessionThreadID == "" {
		primary, found, err := s.db.GetPrimarySessionThreadExternalID(ctx, code.WorkspaceUUID, code.SessionExternalID)
		if err != nil {
			return false, err
		}
		if !found {
			return false, db.ErrNotFound
		}
		request.SessionThreadID = primary
	}
	response, err := selfHostedToolDecision(request, result)
	if err != nil {
		return false, selfHostedResultRoutingError("result_identity", err)
	}
	err = s.sendToolDecision(ctx, code.ExternalID, code.CurrentWorkerEpoch, request, response, "self-hosted-tool-result", request.SessionThreadID, event.ExternalID)
	if err != nil {
		return false, selfHostedResultRoutingError("control_publication", err)
	}
	return true, nil
}
func selfHostedToolDecision(request toolPermissionRequest, result selfHostedToolResult) (map[string]any, error) {
	switch {
	case result.ToolUseID == "" || result.ToolUseID != request.PublicEventID:
		return nil, fmt.Errorf("%w: public tool ID", db.ErrSessionInputConflict)
	case request.EventType != "agent.tool_use":
		return nil, fmt.Errorf("%w: tool event type", db.ErrSessionInputConflict)
	case request.RequestID == "" || request.ToolUseID == "":
		return nil, fmt.Errorf("%w: provider identity", db.ErrSessionInputConflict)
	case result.SessionThreadID != "" && result.SessionThreadID != request.SessionThreadID:
		return nil, fmt.Errorf("%w: thread identity", db.ErrSessionInputConflict)
	case len(result.Content) == 0:
		return nil, fmt.Errorf("%w: empty content", db.ErrSessionInputConflict)
	}
	for _, block := range result.Content {
		if block.Type != "text" {
			return nil, ErrProtocol
		}
	}
	return map[string]any{"behavior": "allow", "updatedInput": request.Input, "externalResult": map[string]any{"content": result.Content, "isError": result.IsError}}, nil
}

func selfHostedPendingResult(metadata []byte, codeSessionID string, result selfHostedToolResult) (toolPermissionRequest, selfHostedToolResult, error) {
	request, err := toolPermissionRequestFromMetadata(metadata, codeSessionID, result.ToolUseID)
	if err != nil {
		return toolPermissionRequest{}, result, err
	}
	result.ToolUseID = request.PublicEventID
	return request, result, nil
}
