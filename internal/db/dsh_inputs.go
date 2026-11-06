package db

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/yourbatis"
)

func insertDSHInputs(ctx context.Context, executor yourbatis.Executor, session Session, code dshCodeRow, events []SessionEvent) (sessionEventBatchResult, error) {
	scope := DSHScope{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionID: session.ExternalID, CodeSessionID: code.ExternalID, CodeSessionUUID: code.UUID}
	worker, found, err := NewCodeSessionMapper(executor).LockCodeSessionByExternalID(ctx, code.ExternalID)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	if !found || worker.OrganizationUUID != session.OrganizationUUID || worker.WorkspaceUUID != session.WorkspaceUUID {
		return sessionEventBatchResult{}, ErrNotFound
	}
	primary, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	mapper := NewDSHExecutionMapper(executor)
	permissions, err := mapper.Permissions(ctx, scope)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	result := sessionEventBatchResult{}
	for _, event := range events {
		if session.Status == "terminated" {
			return result, ErrSessionInputConflict
		}
		switch event.EventType {
		case "user.message":
			if session.BudgetReachedAt != nil || len(dshPendingIDs(permissions, "")) > 0 {
				return result, ErrSessionInputPaused
			}
			if event.ThreadExternalID != nil && *event.ThreadExternalID != primary.ExternalID {
				return result, ErrSessionInputConflict
			}
			if _, err := mapper.InsertExecution(ctx, scope, event.ExternalID, primary.ExternalID, event.CreatedAt); err != nil {
				return result, err
			}
		case "user.tool_confirmation", "user.custom_tool_result", "user.tool_result":
			if err := resolveDSHConfirmation(ctx, executor, scope, permissions, event); err != nil {
				return result, err
			}
		case "user.interrupt":
			thread := ""
			if event.ThreadExternalID != nil {
				thread = *event.ThreadExternalID
			}
			if _, err := mapper.InterruptExecution(ctx, scope, thread); err != nil {
				return result, err
			}
			if _, err := mapper.ClosePermissions(ctx, scope, "", thread); err != nil {
				return result, err
			}
		}
		write, err := insertSessionEventTx(ctx, executor, &session, primary.thread(), event)
		if err != nil {
			return result, err
		}
		if write.Inserted {
			result.Events = append(result.Events, write.Event)
		}
	}
	statusEvents, err := reduceDSHState(ctx, executor, &session, primary.thread(), scope, "accept:"+eventBatchSeed(events))
	result.Events = append(result.Events, statusEvents...)
	if err != nil {
		return result, err
	}
	err = NewCodeSessionMapper(executor).ResetIdleSinceForSession(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID, false)
	return result, err
}

func eventBatchSeed(events []SessionEvent) string {
	ids := make([]string, len(events))
	for i, event := range events {
		ids[i] = event.ExternalID
	}
	raw, _ := json.Marshal(ids)
	return string(raw)
}

type dshConfirmation struct {
	Result       string          `json:"result"`
	DenyMessage  string          `json:"deny_message"`
	UpdatedInput json.RawMessage `json:"updated_input"`
	Content      json.RawMessage `json:"content"`
	IsError      bool            `json:"is_error"`
}

type dshResolvedDecision struct {
	Behavior       string             `json:"behavior"`
	Message        string             `json:"message,omitempty"`
	UpdatedInput   json.RawMessage    `json:"updated_input,omitempty"`
	ExternalResult *dshExternalResult `json:"external_result,omitempty"`
}

type dshExternalResult struct {
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"isError"`
}

func resolveDSHConfirmation(ctx context.Context, executor yourbatis.Executor, scope DSHScope, permissions []dshPermissionRow, event SessionEvent) error {
	if event.ToolUseID == nil || *event.ToolUseID == "" {
		return ErrSessionToolUseNotPending
	}
	var input dshConfirmation
	if err := json.Unmarshal(event.Payload, &input); err != nil {
		return err
	}
	for _, permission := range permissions {
		if permission.State != "pending" || permission.PublicEventID != *event.ToolUseID {
			continue
		}
		if event.ThreadExternalID != nil && *event.ThreadExternalID != permission.ThreadID {
			return ErrSessionToolUseNotPending
		}
		decision := dshResolvedDecision{Behavior: input.Result, Message: input.DenyMessage, UpdatedInput: input.UpdatedInput}
		if event.EventType != "user.tool_confirmation" {
			decision = dshResolvedDecision{Behavior: "external_result", ExternalResult: &dshExternalResult{Content: input.Content, IsError: input.IsError}}
		}
		if decision.Behavior != "allow" && decision.Behavior != "deny" && decision.Behavior != "external_result" {
			return ErrSessionToolConfirmationInvalid
		}
		raw, err := json.Marshal(decision)
		if err != nil {
			return err
		}
		changed, err := NewDSHExecutionMapper(executor).SetPermission(ctx, scope, permission.RequestID, "resolved", raw)
		if err != nil {
			return err
		}
		if changed != 1 {
			return ErrSessionToolUseNotPending
		}
		return nil
	}
	return ErrSessionToolUseNotPending
}

func (tx ManagedAgentActivationTx) AcceptDSHActivationInputs(ctx context.Context, code CodeSession, events []SessionEvent) error {
	scope := DSHScope{CodeSessionID: code.ExternalID, CodeSessionUUID: code.UUID, OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, SessionID: code.SessionExternalID}
	primary, err := tx.dshThreadMapper.FindPrimary(ctx, code.WorkspaceUUID, code.SessionExternalID)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.EventType != "user.message" || !event.ProcessedAt.IsZero() {
			continue
		}
		if _, err := tx.dshMapper.InsertExecution(ctx, scope, event.ExternalID, primary.ExternalID, event.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}
