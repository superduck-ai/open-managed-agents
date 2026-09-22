package sessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type toolReplyReference struct {
	ToolUseID       string `json:"tool_use_id"`
	CustomToolUseID string `json:"custom_tool_use_id"`
}

func (h *Handler) validateToolReply(ctx context.Context, session db.Session, event *db.SessionEvent) error {
	if event.EventType != "user.tool_confirmation" && event.EventType != "user.custom_tool_result" && event.EventType != "user.tool_result" {
		return nil
	}
	var reply toolReplyReference
	if err := jsonv2.Unmarshal(event.Payload, &reply); err != nil {
		return internalError("Could not validate tool reply", err)
	}
	toolID := reply.ToolUseID
	if event.EventType == "user.custom_tool_result" {
		toolID = reply.CustomToolUseID
	}
	tool, err := h.eventPayloads.GetSessionEvent(ctx, session.WorkspaceUUID, session.ExternalID, toolID)
	if errors.Is(err, db.ErrNotFound) {
		return invalidRequest(errors.New("tool reply must reference a pending tool invocation"))
	}
	if err != nil {
		return internalError("Could not validate tool reply", err)
	}
	var invocation struct {
		Name       string `json:"name"`
		Permission string `json:"evaluated_permission"`
		ThreadID   string `json:"session_thread_id"`
	}
	if err := jsonv2.Unmarshal(tool.Payload, &invocation); err != nil {
		return internalError("Could not validate tool reply", err)
	}
	switch event.EventType {
	case "user.tool_confirmation":
		if !slices.Contains([]string{"agent.tool_use", "agent.mcp_tool_use"}, tool.EventType) || invocation.Permission != "ask" {
			return invalidRequest(errors.New("only ask tool invocations accept confirmations"))
		}
	case "user.custom_tool_result":
		if invocation.Name != "AskUserQuestion" {
			return invalidRequest(errors.New("this Worker supports custom tool results only for AskUserQuestion"))
		}
		if err := validateQuestionAnswers(event.Payload); err != nil {
			return invalidRequest(err)
		}
		if tool.EventType != "agent.custom_tool_use" {
			return invalidRequest(errors.New("custom_tool_use_id must reference a custom tool invocation"))
		}
	case "user.tool_result":
		return invalidRequest(errors.New("user.tool_result requires a self-hosted tool executor; this Worker returns agent.tool_result"))
	}
	if invocation.ThreadID != "" {
		event.ThreadExternalID = new(invocation.ThreadID)
	} else {
		event.ThreadExternalID = tool.ThreadExternalID
	}
	if event.ThreadExternalID != nil {
		thread, err := h.db.GetSessionThread(ctx, session.WorkspaceUUID, session.ExternalID, *event.ThreadExternalID)
		if err != nil {
			return internalError("Could not validate tool reply", err)
		}
		if invocation.ThreadID == "" && thread.ParentThreadUUID != nil {
			codeSession, err := h.db.GetCodeSessionBySessionExternalID(ctx, session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				return internalError("Could not validate tool reply", err)
			}
			toolID = derivedSessionEventID(codeSession.ExternalID, toolID, tool.EventType, "primary")
		}
	}
	var fields map[string]json.RawMessage
	if err := jsonv2.Unmarshal(event.Payload, &fields); err != nil {
		return internalError("Could not validate tool reply", err)
	}
	key := "tool_use_id"
	if event.EventType == "user.custom_tool_result" {
		key = "custom_tool_use_id"
	}
	fields[key], err = jsonv2.Marshal(toolID)
	if err != nil {
		return internalError("Could not validate tool reply", err)
	}
	event.Payload, err = jsonv2.Marshal(fields)
	event.ToolUseID = new(toolID)
	if err != nil {
		return internalError("Could not validate tool reply", err)
	}
	return nil
}

func validateQuestionAnswers(raw json.RawMessage) error {
	var result struct {
		IsError bool `json:"is_error"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := jsonv2.Unmarshal(raw, &result); err != nil {
		return err
	}
	var parts []string
	for _, block := range result.Content {
		if block.Type != "text" {
			return errors.New("AskUserQuestion results must contain text answers")
		}
		parts = append(parts, block.Text)
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if result.IsError || text == "" {
		return nil
	}
	var answers map[string]json.RawMessage
	if err := jsonv2.Unmarshal([]byte(text), &answers); err != nil || answers == nil {
		return errors.New("AskUserQuestion answers must be a JSON object")
	}
	return nil
}

func validateInputBatch(events []db.SessionEvent) error {
	seenReplies := make(map[string]struct{})
	for _, event := range events {
		if event.ToolUseID != nil {
			if _, exists := seenReplies[*event.ToolUseID]; exists {
				return errors.New("a tool invocation may be resolved only once per request")
			}
			seenReplies[*event.ToolUseID] = struct{}{}
		}
		if event.EventType == "system.message" {
			return errors.New("this Worker does not support updating system instructions during a session")
		}
	}
	return nil
}
