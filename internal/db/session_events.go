package db

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

type sessionEventBatchResult struct {
	Events            []SessionEvent
	SessionTerminated bool
}

type sessionEventWriteResult struct {
	Event             SessionEvent
	Inserted          bool
	SessionTerminated bool
}

// The caller holds the Session lock. Lock the Worker second, decide acceptance,
// then persist each action's events and state in their public order.
func insertSessionEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent) (sessionEventBatchResult, error) {
	primary, worker, err := lockSessionEventStateTx(ctx, executor, session)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	acceptedID, err := validateSessionInputBatch(primary, worker, events)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	result := sessionEventBatchResult{Events: make([]SessionEvent, 0, len(events)+2)}
	for _, event := range events {
		var batch []SessionEvent
		if event.ExternalID == acceptedID {
			running := event
			running.EventType = "session.status_running"
			running.ExternalID = derivedStatusEventID(event.ExternalID, running.EventType)
			running.Payload = nil
			batch, err = sessionStatusEventsTx(ctx, executor, session, primary, worker, running)
			batch = append(batch, event)
		} else {
			batch, err = sessionEventBatchTx(ctx, executor, session, primary, worker, event)
		}
		if err != nil {
			return sessionEventBatchResult{}, err
		}
		for _, next := range batch {
			write, err := insertSessionEventTx(ctx, executor, &session, primary, next)
			if err != nil {
				return sessionEventBatchResult{}, err
			}
			if write.Inserted {
				result.Events = append(result.Events, write.Event)
				result.SessionTerminated = result.SessionTerminated || write.SessionTerminated
				trackPrimaryThreadStatus(&primary, next)
			}
		}
	}
	if slices.ContainsFunc(result.Events, func(event SessionEvent) bool { return maevents.IsPublicWorkerInputEvent(event.EventType) }) {
		newTurn := slices.ContainsFunc(result.Events, func(event SessionEvent) bool { return event.ExternalID == acceptedID })
		if err := NewCodeSessionMapper(executor).ResetIdleSinceForSession(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID, newTurn); err != nil {
			return sessionEventBatchResult{}, err
		}
	}
	result.SessionTerminated = result.SessionTerminated && session.Status == "terminated"
	return result, nil
}

// History includes events seeded with a new Session and idempotent Worker reports.
// Neither admits a new client turn.
func insertSessionHistoryTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent) (sessionEventBatchResult, error) {
	primary, worker, err := lockSessionEventStateTx(ctx, executor, session)
	if err != nil {
		return sessionEventBatchResult{}, err
	}
	result := sessionEventBatchResult{Events: make([]SessionEvent, 0, len(events)+2)}
	// The Session lock makes proxy finals and Worker echoes compare against one
	// committed source at a time, even when they arrive on different API instances.
	echoKeys := make(map[string]map[string]int)
	for _, event := range events {
		var echo struct {
			RequestID string `json:"model_request_start_id"`
			Source    string `json:"_echo_source"`
			Key       string `json:"_echo_key"`
		}
		if (event.EventType == "agent.message" || event.EventType == "agent.thinking") && len(event.Payload) > 0 {
			if err := jsonv2.Unmarshal(event.Payload, &echo); err != nil {
				return sessionEventBatchResult{}, err
			}
		}
		if echo.RequestID != "" && echo.Source != "" && echo.Key != "" {
			opposite := "worker"
			if echo.Source == "worker" {
				opposite = "proxy"
			}
			cacheKey := echo.RequestID + "\x00" + opposite
			keys, ok := echoKeys[cacheKey]
			if !ok {
				stored, err := NewSessionEventMapper(executor).FindAssistantEchoKeys(ctx, session.WorkspaceUUID, session.ExternalID, echo.RequestID, opposite)
				if err != nil {
					return sessionEventBatchResult{}, err
				}
				keys = make(map[string]int, len(stored))
				for _, key := range stored {
					keys[key]++
				}
				echoKeys[cacheKey] = keys
			}
			if keys[echo.Key] > 0 {
				keys[echo.Key]--
				continue
			}
		}
		_, err := NewSessionEventMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, event.ExternalID)
		if err == nil {
			continue
		}
		if !errors.Is(mapNoRows(err), ErrNotFound) {
			return sessionEventBatchResult{}, err
		}
		batch, err := sessionEventBatchTx(ctx, executor, session, primary, worker, event)
		if err != nil {
			return sessionEventBatchResult{}, err
		}
		for _, next := range batch {
			write, err := insertSessionEventIfAbsentTx(ctx, executor, &session, primary, next)
			if err != nil {
				return sessionEventBatchResult{}, err
			}
			if write.Inserted {
				result.Events = append(result.Events, write.Event)
				result.SessionTerminated = result.SessionTerminated || write.SessionTerminated
				trackPrimaryThreadStatus(&primary, next)
			}
		}
	}
	if slices.ContainsFunc(result.Events, func(event SessionEvent) bool { return maevents.IsPublicWorkerInputEvent(event.EventType) }) {
		if err := NewCodeSessionMapper(executor).ResetIdleSinceForSession(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID, false); err != nil {
			return sessionEventBatchResult{}, err
		}
	}
	result.SessionTerminated = result.SessionTerminated && session.Status == "terminated"
	return result, nil
}

func lockSessionEventStateTx(ctx context.Context, executor yourbatis.Executor, session Session) (SessionThread, codeSessionInputStateRow, error) {
	primaryRow, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return SessionThread{}, codeSessionInputStateRow{}, mapNoRows(err)
	}
	worker, _, err := NewCodeSessionMapper(executor).LockLatestInputState(ctx, session.WorkspaceUUID, session.UUID)
	return primaryRow.thread(), worker, err
}

func sessionEventBatchTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker codeSessionInputStateRow, event SessionEvent) ([]SessionEvent, error) {
	if maevents.CategoryFor(event.EventType) == maevents.CategorySessionStatus || maevents.CategoryFor(event.EventType) == maevents.CategoryThreadStatus {
		return sessionStatusEventsTx(ctx, executor, session, primary, worker, event)
	}
	return []SessionEvent{event}, nil
}

func validateSessionInputBatch(primary SessionThread, worker codeSessionInputStateRow, events []SessionEvent) (string, error) {
	pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, primary.ExternalID)
	if err != nil {
		return "", err
	}
	acceptedID := ""
	for _, event := range events {
		switch event.EventType {
		case "user.message":
			if acceptedID != "" || primary.Status != "idle" || worker.WorkerStatus == "running" || worker.WorkerStatus == "requires_action" || len(pending) > 0 ||
				(event.ThreadExternalID != nil && *event.ThreadExternalID != primary.ExternalID) {
				return "", ErrSessionInputConflict
			}
			acceptedID = event.ExternalID
		case "user.interrupt":
			if primary.Status != "running" && worker.WorkerStatus != "running" && worker.WorkerStatus != "requires_action" {
				return "", ErrSessionInputConflict
			}
		case "user.tool_confirmation", "user.custom_tool_result":
			threadID := primary.ExternalID
			if event.ThreadExternalID != nil {
				threadID = *event.ThreadExternalID
			}
			threadPending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, threadID)
			if err != nil {
				return "", err
			}
			if len(threadPending) == 0 && worker.WorkerStatus != "requires_action" {
				return "", ErrSessionInputConflict
			}
		case "user.tool_result":
			if primary.Status != "running" && worker.WorkerStatus != "requires_action" {
				return "", ErrSessionInputConflict
			}
		}
		if maevents.IsPublicWorkerInputEvent(event.EventType) && event.EventType != "user.message" && acceptedID != "" {
			return "", ErrSessionInputConflict
		}
	}
	return acceptedID, nil
}

func insertSessionEventTx(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, event SessionEvent) (sessionEventWriteResult, error) {
	event, write, err := prepareSessionEventTx(ctx, executor, session, primary, event)
	if err != nil || !write {
		return sessionEventWriteResult{}, err
	}
	row, err := NewSessionEventMapper(executor).Insert(ctx, sessionEventWriteParameters(event))
	if err != nil {
		return sessionEventWriteResult{}, err
	}
	terminated, err := applySessionEventWriteTx(ctx, executor, session, primary, event)
	if err != nil {
		return sessionEventWriteResult{}, err
	}
	return sessionEventWriteResult{Event: row.event(), Inserted: true, SessionTerminated: terminated}, nil
}

func insertSessionEventIfAbsentTx(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, event SessionEvent) (sessionEventWriteResult, error) {
	event, write, err := prepareSessionEventTx(ctx, executor, session, primary, event)
	if err != nil || !write {
		return sessionEventWriteResult{}, err
	}
	row, inserted, err := NewSessionEventMapper(executor).InsertIfAbsent(ctx, sessionEventWriteParameters(event))
	if err != nil || !inserted {
		return sessionEventWriteResult{}, err
	}
	terminated, err := applySessionEventWriteTx(ctx, executor, session, primary, event)
	if err != nil {
		return sessionEventWriteResult{}, err
	}
	return sessionEventWriteResult{Event: row.event(), Inserted: true, SessionTerminated: terminated}, nil
}

func prepareSessionEventTx(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, event SessionEvent) (SessionEvent, bool, error) {
	write, err := shouldWriteSessionStatus(ctx, executor, *session, primary, event)
	if err != nil || !write {
		return SessionEvent{}, false, err
	}
	event.OrganizationUUID, event.WorkspaceUUID = session.OrganizationUUID, session.WorkspaceUUID
	event.SessionUUID, event.SessionExternalID = session.UUID, session.ExternalID
	if event.ThreadExternalID == nil {
		event.ThreadExternalID = &primary.ExternalID
	}
	thread, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, *event.ThreadExternalID)
	if err != nil {
		return SessionEvent{}, false, mapNoRows(err)
	}
	event.ThreadUUID = &thread.UUID
	if event.EventType == "session.usage" {
		if event.Payload, err = sessionUsagePayload(event, session.Usage); err != nil {
			return SessionEvent{}, false, err
		}
	}
	return event, true, nil
}

func applySessionEventWriteTx(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, event SessionEvent) (bool, error) {
	if err := attachEventPayloadBlob(ctx, executor, session.WorkspaceUUID, event.PayloadBlobUUID); err != nil {
		return false, err
	}
	return applySessionEventState(ctx, executor, session, primary.ExternalID, event)
}
