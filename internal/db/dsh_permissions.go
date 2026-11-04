package db

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/jsoncanonical"
	"github.com/superduck-ai/yourbatis"
)

func dshPermissionFromRow(row dshPermissionRow) DSHPermission {
	result := DSHPermission{RequestID: row.RequestID, DSHSessionID: row.DSHSessionID, ToolUseID: row.ToolUseID, ThreadID: row.ThreadID, Tool: row.Tool, Input: row.Input, Available: row.Available, PublicEventID: row.PublicEventID, State: row.State, Decision: row.Decision}
	if row.InputID != nil {
		result.InputID = *row.InputID
	}
	if row.Turn != nil {
		result.Turn = *row.Turn
	}
	return result
}

func (d *DB) RequestDSHPermission(ctx context.Context, scope DSHScope, request DSHPermission, publicEvents []SessionEvent) (DSHPermission, SessionEventChanges, error) {
	var result DSHPermission
	var changes SessionEventChanges
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, primary, err := lockDSHSession(ctx, executor, scope)
		if err != nil {
			return err
		}
		mapper := NewDSHExecutionMapper(executor)
		row, found, err := mapper.Permission(ctx, scope, request.RequestID)
		if err != nil {
			return err
		}
		if found {
			result = dshPermissionFromRow(row)
			if row.InputID == nil {
				return nil
			}
			equal, err := sameDSHPermission(row, request)
			if err != nil {
				return err
			}
			if !equal {
				return ErrDSHPermissionConflict
			}
			return nil
		}
		execution, found, err := mapper.Execution(ctx, scope, request.InputID)
		if err != nil {
			return err
		}
		if !found || execution.Status != "running" {
			return ErrDSHInputNotEligible
		}
		transcript, found, err := NewDSHTranscriptMapper(executor).Find(ctx, scope, request.DSHSessionID)
		if err != nil {
			return err
		}
		if !found {
			return ErrDSHInputNotEligible
		}
		threadID := primary.ExternalID
		if transcript.SessionThreadID != nil {
			threadID = *transcript.SessionThreadID
		}
		if request.DSHSessionID != scope.CodeSessionID && transcript.SessionThreadID == nil {
			return ErrDSHInputNotEligible
		}
		if request.ThreadID != threadID {
			return ErrDSHInputNotEligible
		}
		insert := dshPermissionInsert{Scope: scope, RequestID: request.RequestID, InputID: &request.InputID, DSHSessionID: request.DSHSessionID, ThreadID: threadID, ToolUseID: request.ToolUseID, Turn: &request.Turn, Tool: request.Tool, Input: request.Input, Available: request.Available, PublicEventID: request.PublicEventID, State: request.State, Decision: request.Decision}
		if _, err := mapper.InsertPermission(ctx, insert); err != nil {
			return err
		}
		events, err := insertDSHHistory(ctx, executor, &session, primary, publicEvents)
		if err != nil {
			return err
		}
		changes.Events = events
		events, err = reduceDSHState(ctx, executor, &session, primary, scope, "permission:"+request.RequestID)
		changes.Events = append(changes.Events, events...)
		result = request
		return err
	})
	return result, changes, err
}

func sameDSHPermission(row dshPermissionRow, request DSHPermission) (bool, error) {
	if row.InputID == nil || *row.InputID != request.InputID || row.DSHSessionID != request.DSHSessionID || row.ToolUseID != request.ToolUseID || row.Turn == nil || *row.Turn != request.Turn {
		return false, nil
	}
	if row.Available == nil && request.Available != nil || row.Available != nil && (request.Available == nil || *row.Available != *request.Available) {
		return false, nil
	}
	equal, err := jsoncanonical.Equal(row.Tool, request.Tool)
	if err != nil || !equal {
		return false, err
	}
	return jsoncanonical.Equal(row.Input, request.Input)
}

func (d *DB) CancelDSHPermission(ctx context.Context, scope DSHScope, request DSHPermission) (DSHPermission, SessionEventChanges, error) {
	var result DSHPermission
	var changes SessionEventChanges
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		session, primary, err := lockDSHSession(ctx, executor, scope)
		if err != nil {
			return err
		}
		mapper := NewDSHExecutionMapper(executor)
		row, found, err := mapper.Permission(ctx, scope, request.RequestID)
		if err != nil {
			return err
		}
		if !found && request.InputID != "" {
			execution, eligible, err := mapper.Execution(ctx, scope, request.InputID)
			if err != nil {
				return err
			}
			if !eligible || execution.Status != "running" {
				return ErrDSHInputNotEligible
			}
		}
		if found {
			if request.InputID != "" && row.InputID != nil && *row.InputID != request.InputID {
				return ErrDSHPermissionConflict
			}
			if _, err := mapper.SetPermission(ctx, scope, request.RequestID, "cancelled", nil); err != nil {
				return err
			}
		} else {
			if _, err := mapper.InsertPermission(ctx, dshPermissionInsert{Scope: scope, RequestID: request.RequestID, DSHSessionID: request.DSHSessionID, ThreadID: primary.ExternalID, ToolUseID: request.ToolUseID, PublicEventID: request.PublicEventID, State: "cancelled"}); err != nil {
				return err
			}
		}
		row, _, err = mapper.Permission(ctx, scope, request.RequestID)
		if err != nil {
			return err
		}
		result = dshPermissionFromRow(row)
		changes.Events, err = reduceDSHState(ctx, executor, &session, primary, scope, "cancel:"+request.RequestID)
		return err
	})
	return result, changes, err
}

func (d *DB) GetDSHPermission(ctx context.Context, scope DSHScope, requestID string) (DSHPermission, bool, error) {
	row, found, err := NewDSHExecutionMapper(d.mapperDB).Permission(ctx, scope, requestID)
	return dshPermissionFromRow(row), found, err
}

func (d *DB) ResolvedDSHPermissionForInput(ctx context.Context, scope DSHScope, event SessionEvent) (DSHPermission, bool, error) {
	var input dshConfirmation
	if err := json.Unmarshal(event.Payload, &input); err != nil {
		return DSHPermission{}, false, err
	}
	rows, err := NewDSHExecutionMapper(d.mapperDB).Permissions(ctx, scope)
	if err != nil {
		return DSHPermission{}, false, err
	}
	for _, row := range rows {
		if row.PublicEventID == input.ToolUseID && row.State == "resolved" {
			return dshPermissionFromRow(row), true, nil
		}
	}
	return DSHPermission{}, false, nil
}
