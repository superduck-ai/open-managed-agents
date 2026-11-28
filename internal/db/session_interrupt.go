package db

import (
	"context"
	"encoding/json/v2"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

type sessionInterruptIntent struct {
	InputID string `json:"interrupt_input_id"`
	EventID string `json:"interrupt_event_id"`
}

func bindSessionInterruptsTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker *codeSessionInputStateRow, events []SessionEvent, settled map[string]bool) error {
	for i := range events {
		event := &events[i]
		if event.EventType != "user.interrupt" || settled[event.ExternalID] {
			continue
		}
		if event.ThreadExternalID != nil {
			settled[event.ExternalID] = idleSessionInterrupt(primary, *worker, *event)
			continue
		}
		if session.Status == "terminated" {
			return ErrSessionInputConflict
		}
		pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, "")
		if err != nil {
			return err
		}
		if session.Status == "idle" && len(pending) == 0 && worker.WorkerStatus != "requires_action" {
			settled[event.ExternalID] = true
			continue
		}
		target, err := NewSessionEventMapper(executor).FindCancelableInputID(ctx, session.WorkspaceUUID, session.ExternalID)
		if err != nil {
			return err
		}
		if target == "" {
			continue
		}
		event.boundInterrupt = true
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		payload["_interrupt_input_id"] = target
		event.Payload, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		if worker.ExternalID == "" {
			continue
		}
		patch, err := json.Marshal(sessionInterruptIntent{InputID: target, EventID: event.ExternalID})
		if err != nil {
			return err
		}
		metadata, err := mergeCodeSessionWorkerExternalMetadata(worker.WorkerExternalMetadata, patch)
		if err != nil {
			return err
		}
		if _, err := NewCodeSessionMapper(executor).UpdateWorkerToolMetadata(ctx, session.WorkspaceUUID, worker.ExternalID, metadata, false); err != nil {
			return err
		}
		worker.WorkerExternalMetadata = metadata
	}
	return nil
}

func applyInterruptsAtCompletionTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker *codeSessionInputStateRow, source *SessionEvent) ([]SessionEvent, error) {
	if source.EventType != "session.status_idle" && source.EventType != "session.thread_status_idle" {
		return nil, nil
	}
	var payload struct {
		sessionStatusPayload
		ResultInputID string `json:"_result_input_id"`
	}
	if err := json.Unmarshal(source.Payload, &payload); err != nil {
		return nil, err
	}
	if payload.StopReason != nil && payload.StopReason.Type != "end_turn" && payload.StopReason.Type != "retries_exhausted" {
		return nil, nil
	}
	unboundOnly := worker.WorkerTransport == "claim" && payload.ResultInputID == ""
	scope := source.StatusThreadID
	if scope == "" {
		scope = primary.ExternalID
	}
	mapper := NewSessionEventMapper(executor)
	rows, err := mapper.ListCompletingInterrupts(ctx, session.WorkspaceUUID, session.ExternalID, scope, payload.ResultInputID, unboundOnly, source.ProcessedAt)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	source.completesInterruptedThread = true
	worker.WorkerExternalMetadata, err = clearInterruptedThreadToolsTx(ctx, executor, session, primary.ExternalID, worker.ExternalID, worker.WorkerExternalMetadata, scope)
	if err != nil {
		return nil, err
	}
	threads, err := NewSessionThreadMapper(executor).List(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, "")
	if err != nil {
		return nil, err
	}
	allIdle := len(pending) == 0 && sessionStatusAfterThread(threads, scope, "idle") == "idle"
	events := make([]SessionEvent, 0, len(rows))
	for _, row := range rows {
		threadID, err := interruptThreadID(row.Payload)
		if err != nil {
			return nil, err
		}
		if threadID == "" && !allIdle {
			continue
		}
		updated, applied, err := mapper.MarkInputProcessed(ctx, session.WorkspaceUUID, session.ExternalID, row.ExternalID, source.ProcessedAt)
		if err != nil {
			return nil, err
		}
		if applied {
			events = append(events, updated.event())
		}
	}
	return events, nil
}

func interruptThreadID(payload []byte) (string, error) {
	var scope struct {
		ThreadID string `json:"session_thread_id"`
	}
	err := json.Unmarshal(payload, &scope)
	return scope.ThreadID, err
}

func clearInterruptedThreadToolsTx(ctx context.Context, executor yourbatis.Executor, session Session, primaryID, workerID string, metadata []byte, threadID string) ([]byte, error) {
	if workerID == "" {
		return metadata, nil
	}
	updated, err := maevents.ClearPendingToolRequests(metadata, primaryID, threadID)
	if err != nil {
		return nil, err
	}
	pending, err := maevents.PendingToolEventIDs(updated, primaryID, "")
	if err != nil {
		return nil, err
	}
	_, err = NewCodeSessionMapper(executor).UpdateWorkerToolMetadata(ctx, session.WorkspaceUUID, workerID, updated, len(pending) == 0)
	return updated, err
}

func pendingInterruptBlocksIdle(ctx context.Context, executor yourbatis.Executor, session Session, threadID string, event SessionEvent) (bool, error) {
	var payload sessionStatusPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, err
	}
	if payload.StopReason == nil || payload.StopReason.Type != "end_turn" {
		return false, nil
	}
	return NewSessionEventMapper(executor).HasPendingInterrupts(ctx, session.WorkspaceUUID, session.ExternalID, threadID)
}
