package codesessions

import (
	"encoding/json"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type dshPublicSchema struct {
	ID             string           `json:"id"`
	Type           string           `json:"type"`
	ThreadID       string           `json:"session_thread_id"`
	DSHSessionID   string           `json:"dsh_session_id"`
	ParentThreadID string           `json:"parent_thread_id"`
	ToolUseID      string           `json:"tool_use_id"`
	Tool           dsh.ToolIdentity `json:"tool"`
	Content        json.RawMessage  `json:"content"`
	IsError        bool             `json:"is_error"`
	Event          *struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"event"`
	EventID       string          `json:"event_id"`
	Delta         json.RawMessage `json:"delta"`
	AgentSnapshot json.RawMessage `json:"agent_snapshot"`
	Error         json.RawMessage `json:"error"`
}

func prepareDSHPublicEvents(scope db.DSHScope, raws []json.RawMessage) ([]db.DSHPublicEvent, []json.RawMessage, error) {
	batch := make([]db.DSHPublicEvent, 0, len(raws))
	previews := make([]json.RawMessage, 0)
	for _, raw := range raws {
		var schema dshPublicSchema
		if err := json.Unmarshal(raw, &schema); err != nil || schema.ID == "" {
			return nil, nil, db.ErrInvalidState
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, nil, db.ErrInvalidState
		}
		kind, valid := dshPublicType(schema)
		if !valid {
			return nil, nil, db.ErrInvalidState
		}
		setRawJSONField(fields, "type", kind)
		if kind == "event_start" || kind == "event_delta" {
			converted, err := json.Marshal(fields)
			if err != nil {
				return nil, nil, err
			}
			previews = append(previews, converted)
			continue
		}
		publicID := stablePublicEventID(scope.CodeSessionID, schema.ID)
		if schema.Type == "agent.message" || schema.Type == "agent.thinking" {
			publicID = schema.ID
		}
		setRawJSONField(fields, "id", publicID)
		if schema.Type == "tool.result" {
			reference := maevents.ToolUseEventID(scope.CodeSessionID, schema.ToolUseID, schema.ThreadID)
			field := "tool_use_id"
			if kind == "agent.mcp_tool_result" {
				field = "mcp_tool_use_id"
			}
			delete(fields, "tool_use_id")
			setRawJSONField(fields, field, reference)
		}
		delete(fields, "tool")
		if kind == "agent.thinking" {
			delete(fields, "content")
		}
		event, err := dshPublicEvent(scope, fields, publicID, kind, schema.ThreadID)
		if err != nil {
			return nil, nil, err
		}
		item := db.DSHPublicEvent{ID: schema.ID, Payload: raw, Events: []db.SessionEvent{event}, DSHSessionID: schema.DSHSessionID}
		if schema.Type == "session.thread_created" {
			snapshot := schema.AgentSnapshot
			if len(snapshot) == 0 {
				return nil, nil, db.ErrInvalidState
			}
			item.Thread = &db.SessionThread{ExternalID: schema.ThreadID, AgentSnapshot: snapshot, Status: "idle", Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`), CreatedAt: event.CreatedAt}
			item.Events[0].ThreadExternalID = nil
		}
		batch = append(batch, item)
	}
	return batch, previews, nil
}

func dshPublicType(value dshPublicSchema) (string, bool) {
	switch value.Type {
	case "preview.start":
		return "event_start", value.Event != nil && value.Event.ID != "" && (value.Event.Type == "agent.message" || value.Event.Type == "agent.thinking")
	case "preview.delta":
		return "event_delta", value.EventID != "" && len(value.Delta) > 0 && value.Delta[0] == '{'
	case "agent.message", "system.message":
		return value.Type, len(value.Content) > 0 && value.Content[0] == '['
	case "agent.thinking":
		return value.Type, len(value.Content) == 0 || value.Content[0] == '['
	case "tool.result":
		kind := "agent.tool_result"
		if value.Tool.Kind == "mcp" {
			kind = "agent.mcp_tool_result"
		}
		return kind, value.ToolUseID != "" && value.Tool.Name != "" && (value.Tool.Kind == "builtin" || value.Tool.Kind == "mcp" && value.Tool.Server != "" || value.Tool.Kind == "custom" || value.Tool.Kind == "delegation") && len(value.Content) > 0 && value.Content[0] == '['
	case "session.thread_created":
		return value.Type, value.ThreadID != "" && value.DSHSessionID != "" && len(value.AgentSnapshot) > 0 && value.AgentSnapshot[0] == '{'
	case "session.thread_status_running", "session.thread_status_idle", "session.thread_status_terminated":
		return value.Type, value.ThreadID != ""
	case "agent.thread_message_sent", "agent.thread_message_received", "agent.thread_context_compacted":
		return value.Type, true
	case "session.error":
		return value.Type, len(value.Error) > 0 && value.Error[0] == '{'
	}
	return "", false
}

func dshPublicEvent(scope db.DSHScope, fields map[string]json.RawMessage, id, kind, thread string) (db.SessionEvent, error) {
	raw, err := json.Marshal(fields)
	at := time.Now().UTC()
	event := db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionExternalID: scope.SessionID, EventType: kind, Payload: raw, CreatedAt: at, ProcessedAt: at, StatusThreadID: thread}
	if thread != "" {
		event.ThreadExternalID = &thread
	}
	return event, err
}

func dshPermissionEvents(scope db.DSHScope, request toolPermissionRequest, raw json.RawMessage) ([]db.SessionEvent, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var id string
	if err := json.Unmarshal(fields["id"], &id); err != nil {
		return nil, err
	}
	child, err := dshPublicEvent(scope, fields, id, request.EventType, request.SessionThreadID)
	if err != nil {
		return nil, err
	}
	if request.PublicEventID == id {
		return []db.SessionEvent{child}, nil
	}
	setRawJSONField(fields, "id", request.PublicEventID)
	setRawJSONField(fields, "original_event_id", id)
	primary, err := dshPublicEvent(scope, fields, request.PublicEventID, request.EventType, "")
	return []db.SessionEvent{child, primary}, err
}
