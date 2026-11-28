package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/yourbatis"
)

const harnessInterruptedToolText = "The user interrupted this call."

func harnessInterruptEvent(session Session, event SessionEvent) bool {
	return session.ExecutionRuntime == sessioncontract.CloudHarnessRuntime && event.EventType == "user.interrupt"
}

func insertHarnessInterruptTx(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, worker *codeSessionInputStateRow, event SessionEvent) ([]SessionEvent, error) {
	if primary.Status == "terminated" {
		return nil, ErrSessionInterruptTargetInvalid
	}
	write, err := insertSessionEventTx(ctx, executor, session, primary, event)
	if err != nil || !write.Inserted {
		return nil, err
	}
	events := []SessionEvent{write.Event}
	childOnly := event.ThreadExternalID != nil && *event.ThreadExternalID != primary.ExternalID
	row, found, err := NewCloudHarnessMapper(executor).LockCurrentTurn(ctx, harnessInputRef(*session, ""))
	if err != nil {
		return events, err
	}
	if !found || !row.active() {
		if childOnly {
			return events, nil
		}
		stopped, err := cancelQueuedHarnessInputTx(ctx, executor, *session)
		return append(events, stopped...), err
	}
	child, err := harnessSubagentThread(ctx, executor, row)
	if err != nil {
		return nil, err
	}
	if childOnly && (child == nil || child.ExternalID != *event.ThreadExternalID) {
		return events, nil
	}
	stopped, err := interruptHarnessTurnTx(ctx, executor, *session, primary, worker, row, child, childOnly, event.ProcessedAt)
	return append(events, stopped...), err
}

func cancelQueuedHarnessInputTx(ctx context.Context, executor yourbatis.Executor, session Session) ([]SessionEvent, error) {
	ref := harnessInputRef(session, "")
	mapper := NewHarnessInputMapper(executor)
	inputUUID, exists, err := mapper.FindFirstPendingInput(ctx, ref)
	if err != nil || !exists {
		return nil, err
	}
	ref.UUID = inputUUID
	if err := requireHarnessLease(mapper.SetQueuedState(ctx, ref, harnessInputCancelled)); err != nil {
		return nil, err
	}
	return settleHarnessInputTx(ctx, executor, session, inputUUID, HarnessInterrupted)
}

func interruptHarnessTurnTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker *codeSessionInputStateRow, row harnessTurnRow, child *SessionThread, childOnly bool, at time.Time) ([]SessionEvent, error) {
	mapper := NewCloudHarnessMapper(executor)
	ref := row.reference()
	var code CodeSession
	if row.WorkerEpoch != 0 {
		record, err := NewCodeSessionMapper(executor).FindLatestBySessionExternalID(ctx, session.WorkspaceUUID, session.ExternalID)
		if err != nil {
			return nil, mapNoRows(err)
		}
		code = record.session()
	}
	if err := cancelHarnessSandboxTask(ctx, executor, row, code, child); err != nil {
		return nil, err
	}
	events, err := harnessCancelledModelEvents(ctx, executor, session, primary, code, row, child, at)
	if err != nil {
		return nil, err
	}
	if err := stopHarnessHistory(ctx, executor, row); err != nil {
		return nil, err
	}
	params := harnessInterruptParams{Ref: ref, State: HarnessInterrupted}
	if child != nil {
		childEvents, err := finishInterruptedDelegation(ctx, executor, primary, code, row, *child, at, &params, childOnly)
		if err != nil {
			return nil, err
		}
		events = append(events, childEvents...)
	}
	if !childOnly {
		if err := clearHarnessInterruptChoices(ctx, executor, session, primary, worker); err != nil {
			return nil, err
		}
	}
	if err := requireHarnessLease(mapper.InterruptTurn(ctx, params)); err != nil {
		return nil, err
	}
	if code.UUID != "" {
		lease := HarnessTurnLease{Ref: ref, CodeSessionUUID: code.UUID, Epoch: row.WorkerEpoch}
		if err := mapper.RevokeModelLease(ctx, lease); err != nil {
			return nil, err
		}
	}
	batch, err := insertSessionHistoryTx(ctx, executor, session, events)
	if err != nil || childOnly {
		return batch.Events, err
	}
	final, err := settleHarnessInputTx(ctx, executor, session, row.InputEventUUID, HarnessInterrupted)
	return append(batch.Events, final...), err
}

func finishInterruptedDelegation(ctx context.Context, executor yourbatis.Executor, primary SessionThread, code CodeSession, row harnessTurnRow, child SessionThread, at time.Time, params *harnessInterruptParams, childOnly bool) ([]SessionEvent, error) {
	if row.DelegationToolUUID == nil {
		return nil, ErrInvalidState
	}
	const report = "The user interrupted the subagent."
	mapper := NewCloudHarnessMapper(executor)
	tool, found, err := mapper.FindTool(ctx, row.reference(), *row.DelegationToolUUID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrInvalidState
	}
	result := harnessSubagentResultText(child.ExternalID, report)
	if err := requireHarnessLease(mapper.FinishTool(ctx, harnessToolResultParams{
		Ref: row.reference(), ToolUUID: tool.UUID, Text: result, Failed: true,
	})); err != nil {
		return nil, err
	}
	if childOnly {
		params.State, params.TaskUUID, params.ToolUUID, params.Kind = "pending", new(uuid.NewString()), &tool.UUID, HarnessModelTask
	} else {
		row.SubagentThreadUUID, row.DelegationToolUUID, row.ToolUUID = nil, nil, &tool.UUID
		row.TaskKind = HarnessModelTask
		if err := stopHarnessHistory(ctx, executor, row); err != nil {
			return nil, err
		}
	}
	return harnessSubagentReportEvents(code, primary, child, tool, report, result, HarnessInterrupted, at)
}

func clearHarnessInterruptChoices(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker *codeSessionInputStateRow) error {
	metadata, err := maevents.ClearPendingToolRequests(worker.WorkerExternalMetadata, primary.ExternalID, "")
	if err != nil || worker.ExternalID == "" {
		return err
	}
	if _, err := NewCodeSessionMapper(executor).UpdateWorkerToolMetadata(ctx, session.WorkspaceUUID, worker.ExternalID, metadata, true); err != nil {
		return err
	}
	worker.WorkerExternalMetadata = metadata
	return nil
}

func stopHarnessHistory(ctx context.Context, executor yourbatis.Executor, row harnessTurnRow) error {
	key := row.historyKey()
	conversation := NewHarnessConversationMapper(executor)
	state, err := lockHarnessThreadState(ctx, executor, key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	state.Choices = nil
	historyTurnUUID := row.historyTurnUUID()
	if err := discardUnvalidatedHarnessHistory(ctx, conversation, key, historyTurnUUID, &state); err != nil {
		return err
	}
	if row.TaskKind == HarnessToolTask || row.TaskKind == HarnessModelTask {
		result, err := stopHarnessPendingTool(ctx, NewCloudHarnessMapper(executor), row)
		if err != nil {
			return err
		}
		state.MessageCount, err = appendHarnessMessages(ctx, conversation, key, &historyTurnUUID, state.MessageCount, []harness.MessageWithID{result})
		if err != nil {
			return err
		}
	}
	state.Version++
	return saveHarnessTransition(ctx, executor, key, state)
}

func stopHarnessPendingTool(ctx context.Context, mapper CloudHarnessMapper, row harnessTurnRow) (harness.MessageWithID, error) {
	tool, err := harnessHistoryTool(ctx, mapper, row)
	if err != nil {
		return harness.MessageWithID{}, err
	}
	if tool.Result == nil {
		tool.Result, tool.Failed = new(harnessInterruptedToolText), true
		if err := requireHarnessLease(mapper.FinishTool(ctx, harnessToolResultParams{
			Ref: row.reference(), ToolUUID: tool.UUID, Text: *tool.Result, Failed: true,
		})); err != nil {
			return harness.MessageWithID{}, err
		}
	}
	return harnessHistoryToolResult(tool), nil
}

func cancelHarnessSandboxTask(ctx context.Context, executor yourbatis.Executor, row harnessTurnRow, code CodeSession, child *SessionThread) error {
	if row.TaskKind != HarnessToolTask || row.ToolUUID == nil || code.Status != "active" {
		return nil
	}
	threadID := "root"
	if child != nil {
		threadID = child.ExternalID
	}
	_, err := cancelSandboxToolCallTx(ctx, NewSandboxToolCallMapper(executor), SandboxToolCallScope{
		OrganizationUUID: row.OrganizationUUID, WorkspaceUUID: row.WorkspaceUUID, CodeSessionID: code.ExternalID,
		ThreadID: threadID, CallID: *row.ToolUUID, WorkerEpoch: code.CurrentWorkerEpoch,
	})
	return err
}

func harnessCancelledModelEvents(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, code CodeSession, row harnessTurnRow, child *SessionThread, at time.Time) ([]SessionEvent, error) {
	if code.UUID == "" || row.State != "running" || row.TaskKind == HarnessToolTask {
		return nil, nil
	}
	startID := maevents.HarnessModelRequestID(code.ExternalID, row.UUID, row.WorkerEpoch)
	mapper := NewSessionEventMapper(executor)
	start, err := mapper.FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, startID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	eventID := startID + "_end"
	if _, err := mapper.FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, eventID); !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	owner := primary.ExternalID
	if child != nil {
		owner = child.ExternalID
	}
	var original struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(start.Payload, &original); err != nil {
		return nil, err
	}
	payload := struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		StartID   string `json:"model_request_start_id"`
		RequestID string `json:"request_id"`
		Model     string `json:"model"`
		IsError   bool   `json:"is_error"`
		Error     struct {
			Type string `json:"type"`
		} `json:"error"`
		EventIDs []string `json:"event_ids"`
	}{ID: eventID, Type: "span.model_request_end", StartID: startID, RequestID: startID, Model: original.Model, IsError: true,
		EventIDs: []string{maevents.StablePublicEventID(code.ExternalID, startID+":message")}}
	payload.Error.Type = "cancelled"
	event, err := newHarnessSubagentEvent(eventID, payload.Type, &owner, payload, at)
	return []SessionEvent{event}, err
}
