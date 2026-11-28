package db

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/yourbatis"
)

func parkHarnessChoiceTx(ctx context.Context, executor yourbatis.Executor, session Session, lease HarnessTurnLease, row harnessTurnRow, tool HarnessTool, events []SessionEvent) ([]SessionEvent, error) {
	eventType := "agent.tool_use"
	if tool.Name == sessioncontract.HarnessAskUserQuestion {
		eventType = "agent.custom_tool_use"
	}
	index := slices.IndexFunc(events, func(event SessionEvent) bool { return event.EventType == eventType })
	if index < 0 {
		return nil, ErrInvalidState
	}
	event := events[index]
	if tool.Name == sessioncontract.HarnessAskUserQuestion {
		if err := saveHarnessQuestion(ctx, executor, session, lease, tool, event); err != nil {
			return nil, err
		}
	}
	if err := checkpointHarnessTool(ctx, NewCloudHarnessMapper(executor), lease, row, tool, HarnessWaitingChoice); err != nil {
		return nil, err
	}
	if err := setHarnessChoiceMetadata(ctx, executor, session, tool, event); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(sessionStatusPayload{StopReason: &sessionStopReason{Type: "requires_action", EventIDs: []string{event.ExternalID}}})
	if err != nil {
		return nil, err
	}
	batch, err := insertSessionHistoryTx(ctx, executor, session, []SessionEvent{{
		UUID: uuid.NewString(), ExternalID: derivedStatusEventID(event.ExternalID, "session.status_idle"),
		EventType: "session.status_idle", Payload: payload, CreatedAt: event.CreatedAt, ProcessedAt: event.CreatedAt,
	}})
	return batch.Events, err
}

func saveHarnessQuestion(ctx context.Context, executor yourbatis.Executor, session Session, lease HarnessTurnLease, tool HarnessTool, event SessionEvent) error {
	question, err := sessioncontract.ParseHarnessChoiceQuestion(tool.Arguments)
	if err != nil {
		return err
	}
	options := make([]string, 0, len(question.Options))
	for _, option := range question.Options {
		options = append(options, option.Label)
	}
	key := harnessRootKey(lease.Ref)
	state, err := lockHarnessThreadState(ctx, executor, key)
	if err != nil {
		return err
	}
	if len(state.Choices) != 0 {
		return ErrInvalidState
	}
	state.Choices = []harness.UserChoice{{ID: harness.ChoiceID(event.ExternalID), Prompt: question.Question, Choices: options, ResponseURL: "/v1/sessions/" + session.ExternalID + "/events"}}
	return saveHarnessState(ctx, NewHarnessStateMapper(executor), key, state)
}

func setHarnessChoiceMetadata(ctx context.Context, executor yourbatis.Executor, session Session, tool HarnessTool, event SessionEvent) error {
	workers := NewCodeSessionMapper(executor)
	worker, found, err := workers.LockLatestInputState(ctx, session.WorkspaceUUID, session.UUID)
	if err != nil {
		return err
	}
	if !found || worker.Status != "active" {
		return ErrInvalidState
	}
	type request struct {
		PublicEventID string `json:"public_event_id"`
		RequestID     string `json:"request_id"`
		ToolUseID     string `json:"provider_tool_use_id"`
	}
	patch, err := json.Marshal(map[string]request{
		maevents.ToolPermissionRequestMetadataKey + ":" + event.ExternalID: {PublicEventID: event.ExternalID, RequestID: event.UUID, ToolUseID: tool.CallID},
	})
	if err != nil {
		return err
	}
	metadata, err := mergeCodeSessionWorkerExternalMetadata(worker.WorkerExternalMetadata, patch)
	if err != nil {
		return err
	}
	_, err = workers.UpdateWorkerToolMetadata(ctx, session.WorkspaceUUID, worker.ExternalID, metadata, false)
	return err
}

func acceptHarnessChoiceTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker *codeSessionInputStateRow, event SessionEvent) ([]SessionEvent, error) {
	ref := HarnessTurnRef{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID}
	mapper := NewCloudHarnessMapper(executor)
	row, found, err := mapper.LockWaitingChoice(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !found || row.ToolUUID == nil {
		return nil, ErrSessionToolUseNotPending
	}
	ref = row.reference()
	tool, found, err := mapper.FindTool(ctx, ref, *row.ToolUUID)
	if err != nil {
		return nil, err
	}
	if !found || event.ToolUseID == nil {
		return nil, ErrSessionToolUseNotPending
	}
	if event.EventType == "user.tool_confirmation" {
		if tool.Name == sessioncontract.HarnessAskUserQuestion || maevents.ToolUseEventID(worker.ExternalID, tool.CallID, "") != *event.ToolUseID {
			return nil, ErrSessionToolUseNotPending
		}
		var confirmation struct {
			Result      string `json:"result"`
			DenyMessage string `json:"deny_message"`
		}
		if json.Unmarshal(event.Payload, &confirmation) != nil || (confirmation.Result != "allow" && confirmation.Result != "deny") {
			return nil, ErrSessionInputConflict
		}
		if confirmation.Result != "deny" {
			confirmation.DenyMessage = ""
		}
		if err := requireHarnessLease(mapper.ResumeApproval(ctx, ref, event.UUID, confirmation.Result, confirmation.DenyMessage)); err != nil {
			return nil, err
		}
		event.ProcessedAt = event.CreatedAt
	} else if err := finishHarnessQuestion(ctx, executor, mapper, ref, tool, event); err != nil {
		return nil, err
	}
	metadata, err := maevents.SettlePendingToolRequest(worker.WorkerExternalMetadata, worker.ExternalID, primary.ExternalID, primary.ExternalID, *event.ToolUseID)
	if err != nil {
		return nil, err
	}
	if _, err := NewCodeSessionMapper(executor).UpdateWorkerToolMetadata(ctx, session.WorkspaceUUID, worker.ExternalID, metadata, true); err != nil {
		return nil, err
	}
	worker.WorkerExternalMetadata = metadata
	running := event
	running.UUID = uuid.NewString()
	running.EventType = "session.status_running"
	running.ExternalID = derivedStatusEventID(event.ExternalID, running.EventType)
	running.Payload = nil
	status, err := sessionStatusEventsTx(ctx, executor, session, primary, worker, running)
	return append(status, event), err
}

func clearHarnessChoice(ctx context.Context, executor yourbatis.Executor, ref HarnessTurnRef, choiceID string) error {
	key := harnessRootKey(ref)
	state, err := lockHarnessThreadState(ctx, executor, key)
	if err != nil {
		return err
	}
	if len(state.Choices) != 1 || string(state.Choices[0].ID) != choiceID {
		return ErrSessionToolUseNotPending
	}
	state.Choices = nil
	return saveHarnessState(ctx, NewHarnessStateMapper(executor), key, state)
}

func finishHarnessQuestion(ctx context.Context, executor yourbatis.Executor, mapper CloudHarnessMapper, ref HarnessTurnRef, tool harnessToolRow, event SessionEvent) error {
	answer, err := sessioncontract.ParseHarnessChoiceResponse(event.Payload)
	if err != nil {
		return err
	}
	if tool.Name != sessioncontract.HarnessAskUserQuestion {
		return ErrSessionToolUseNotPending
	}
	question, err := sessioncontract.ParseHarnessChoiceQuestion(tool.Arguments)
	if err != nil {
		return err
	}
	if !question.Accepts(answer) {
		return ErrSessionInputConflict
	}
	if err := clearHarnessChoice(ctx, executor, ref, answer.ID); err != nil {
		return err
	}
	if err := requireHarnessLease(mapper.FinishTool(ctx, harnessToolResultParams{
		Ref: ref, ToolUUID: tool.UUID, Text: answer.Text, Failed: answer.Failed,
	})); err != nil {
		return err
	}
	if err := requireHarnessLease(mapper.ResumeChoice(ctx, ref, event.UUID)); err != nil {
		return err
	}
	return nil
}
