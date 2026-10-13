package codesessions

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type subagentLifecycle struct {
	Type       string `json:"type"`
	TaskID     string `json:"task_id"`
	ToolUseID  string `json:"tool_use_id"`
	ThreadID   string `json:"session_thread_id"`
	AgentID    string `json:"agent_id"`
	AgentIDAlt string `json:"agentId"`
}

func (s *Service) ReconcileSubagentSnapshots(ctx context.Context, codeSession db.CodeSession) error {
	lifecycles, err := s.listSubagentThreadCreatedEvents(ctx, codeSession)
	if err != nil {
		return err
	}
	for _, lifecycle := range lifecycles {
		if lifecycle.ThreadID == "" || lifecycle.ToolUseID == "" {
			continue
		}
		thread, err := s.db.GetSessionThread(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, lifecycle.ThreadID)
		if err != nil {
			return err
		}
		var identity struct {
			ID string `json:"id"`
		}
		if err := jsonv2.Unmarshal(thread.AgentSnapshot, &identity); err != nil {
			return err
		}
		if identity.ID != "" {
			continue
		}
		snapshot, err := s.subagentSnapshotForTool(ctx, codeSession, lifecycle.ToolUseID)
		if err != nil {
			return err
		}
		if len(snapshot) == 0 {
			continue
		}
		if err := s.db.FillSessionThreadAgentSnapshot(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, lifecycle.ThreadID, snapshot); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) resolveSubagentThread(ctx context.Context, codeSession db.CodeSession, threadID string) (db.SessionThread, error) {
	thread, err := s.db.GetSessionThread(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, threadID)
	if err != nil || thread.ParentThreadUUID == nil {
		return thread, err
	}
	var identity struct {
		ID string `json:"id"`
	}
	if err := jsonv2.Unmarshal(thread.AgentSnapshot, &identity); err != nil {
		return thread, err
	}
	if identity.ID != "" {
		return thread, nil
	}
	session, found, err := s.db.GetSession(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return thread, err
	}
	if !found {
		return thread, db.ErrNotFound
	}
	var parent struct {
		Multiagent *struct{} `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(session.AgentSnapshot, &parent); err != nil {
		return thread, err
	}
	if parent.Multiagent == nil {
		thread.AgentSnapshot = session.AgentSnapshot
		return thread, nil
	}
	if err := s.ReconcileSubagentSnapshots(ctx, codeSession); err != nil {
		return thread, err
	}
	thread, err = s.db.GetSessionThread(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, threadID)
	if err != nil {
		return thread, err
	}
	if err := jsonv2.Unmarshal(thread.AgentSnapshot, &identity); err != nil {
		return thread, err
	}
	if identity.ID == "" {
		return thread, db.ErrInvalidState
	}
	return thread, nil
}

func (s *Service) enrichSubagentLifecycle(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) ([]json.RawMessage, error) {
	for i, raw := range payloads {
		var lifecycle subagentLifecycle
		if err := jsonv2.Unmarshal(raw, &lifecycle); err != nil {
			return nil, err
		}
		if lifecycle.TaskID == "" || lifecycle.ThreadID == "" {
			continue
		}
		mappings, err := s.subagentThreadMappings(ctx, codeSession)
		if err != nil {
			return nil, err
		}
		fields, err := decodeRawJSONObject(raw)
		if err != nil {
			return nil, err
		}
		if previous := mappings[lifecycle.TaskID]; previous != "" {
			setRawJSONField(fields, "session_thread_id", previous)
		}
		if lifecycle.Type == "session.thread_created" {
			snapshot, err := s.subagentSnapshotForTool(ctx, codeSession, lifecycle.ToolUseID)
			if err != nil {
				return nil, err
			}
			if len(snapshot) > 0 {
				fields["agent"] = snapshot
			}
		}
		payloads[i], err = jsonv2.Marshal(fields)
		if err != nil {
			return nil, err
		}
	}
	return payloads, nil
}

func (s *Service) subagentSnapshotForTool(ctx context.Context, codeSession db.CodeSession, toolUseID string) (json.RawMessage, error) {
	if toolUseID == "" {
		return nil, nil
	}
	event, err := s.eventPayloads.GetSessionEvent(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID, maevents.ToolUseEventID(codeSession.ExternalID, toolUseID))
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tool struct {
		Input struct {
			SubagentType string `json:"subagent_type"`
		} `json:"input"`
	}
	if err := jsonv2.Unmarshal(event.Payload, &tool); err != nil {
		return nil, err
	}
	if tool.Input.SubagentType == "" {
		return nil, nil
	}
	session, found, err := s.db.GetSession(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	var root struct {
		Multiagent *struct{} `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(session.AgentSnapshot, &root); err != nil {
		return nil, err
	}
	if root.Multiagent == nil {
		return nil, nil
	}
	return agentsnapshot.ForSubagentType(session.AgentSnapshot, tool.Input.SubagentType)
}
