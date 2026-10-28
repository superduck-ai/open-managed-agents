package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/yourbatis"
)

func stampWebSearchPrice(ctx context.Context, executor yourbatis.Executor, event SessionEvent) (SessionEvent, error) {
	id, ok := webSearchResultID(event)
	if !ok {
		return event, nil
	}
	row, err := NewSessionEventMapper(executor).FindByExternalID(ctx, event.WorkspaceUUID, event.SessionExternalID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return event, nil
		}
		return event, err
	}
	var call struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(row.Payload, &call) != nil || call.Name != "web_search" {
		return event, nil
	}
	stamped, err := mergeToolPrice(event.Payload)
	if err != nil {
		return event, err
	}
	event.Payload = stamped
	return event, nil
}

func webSearchResultID(event SessionEvent) (string, bool) {
	if event.EventType != "agent.tool_result" || event.WorkspaceUUID == "" || event.SessionExternalID == "" {
		return "", false
	}
	var body struct {
		IsError   json.RawMessage `json:"is_error"`
		ToolUseID string          `json:"tool_use_id"`
		ToolPrice json.RawMessage `json:"tool_price"`
	}
	if len(event.Payload) > 0 && json.Unmarshal(event.Payload, &body) != nil {
		return "", false
	}
	if string(body.IsError) == "true" || string(body.IsError) == `"true"` || len(body.ToolPrice) > 0 {
		return "", false
	}
	id := body.ToolUseID
	if event.ToolUseID != nil && *event.ToolUseID != "" {
		id = *event.ToolUseID
	}
	return id, id != ""
}

func mergeToolPrice(payload json.RawMessage) (json.RawMessage, error) {
	body := map[string]json.RawMessage{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &body); err != nil {
			return payload, nil
		}
	}
	raw, err := json.Marshal(billing.CurrentSearchPrice())
	if err != nil {
		return nil, err
	}
	body["tool_price"] = raw
	return json.Marshal(body)
}
