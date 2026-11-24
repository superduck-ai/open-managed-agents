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
	index := slices.IndexFunc(events, func(event SessionEvent) bool { return event.EventType == "agent.custom_tool_use" })
	if index < 0 {
		return nil, ErrInvalidState
	}
	event := events[index]
	question, err := sessioncontract.ParseHarnessChoiceQuestion(tool.Arguments)
	if err != nil {
		return nil, err
	}
	options := make([]string, 0, len(question.Options))
	for _, option := range question.Options {
		options = append(options, option.Label)
	}
	key := harnessRootKey(lease.Ref)
	state, err := lockHarnessThreadState(ctx, executor, key)
	if err != nil {
		return nil, err
	}
	if len(state.Choices) != 0 {
		return nil, ErrInvalidState
	}
	state.Choices = []harness.UserChoice{{ID: harness.ChoiceID(event.ExternalID), Prompt: question.Question, Choices: options, ResponseURL: "/v1/sessions/" + session.ExternalID + "/events"}}
	if err := saveHarnessState(ctx, NewHarnessStateMapper(executor), key, state); err != nil {
		return nil, err
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
	answer, err := sessioncontract.ParseHarnessChoiceResponse(event.Payload)
	if err != nil {
		return nil, err
	}
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
	if !found || tool.Name != sessioncontract.HarnessAskUserQuestion {
		return nil, ErrInvalidState
	}
	question, err := sessioncontract.ParseHarnessChoiceQuestion(tool.Arguments)
	if err != nil {
		return nil, err
	}
	if !question.Accepts(answer) {
		return nil, ErrSessionInputConflict
	}
	if err := clearHarnessChoice(ctx, executor, ref, answer.ID); err != nil {
		return nil, err
	}
	if err := requireHarnessLease(mapper.FinishTool(ctx, harnessToolResultParams{
		Ref: ref, ToolUUID: tool.UUID, Text: answer.Text, Failed: answer.Failed,
	})); err != nil {
		return nil, err
	}
	if err := requireHarnessLease(mapper.ResumeChoice(ctx, ref, event.UUID)); err != nil {
		return nil, err
	}
	metadata, err := maevents.SettlePendingToolRequest(worker.WorkerExternalMetadata, worker.ExternalID, primary.ExternalID, primary.ExternalID, answer.ID)
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
