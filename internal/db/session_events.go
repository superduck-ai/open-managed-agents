package db

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"slices"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

// The caller holds the Session lock. Lock the Worker second, decide acceptance,
// then persist each action's events and state in their public order.
func insertSessionEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, events []SessionEvent, ignoreExisting bool) ([]SessionEvent, error) {
	primaryRow, err := NewSessionThreadMapper(executor).FindPrimary(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, mapNoRows(err)
	}
	primary := primaryRow.thread()
	worker, _, err := NewCodeSessionMapper(executor).LockLatestInputState(ctx, session.WorkspaceUUID, session.UUID)
	if err != nil {
		return nil, err
	}
	acceptedID, err := idlePrimaryInputID(ctx, executor, session, primary, worker, events)
	if err != nil {
		return nil, err
	}
	created := make([]SessionEvent, 0, len(events)+2)
	// The Session lock makes proxy finals and Worker echoes compare against one
	// committed source at a time, even when they arrive on different API instances.
	echoKeys := make(map[string]map[string]int)
	for _, event := range events {
		var echo struct {
			RequestID string `json:"model_request_start_id"`
			Source    string `json:"_echo_source"`
			Key       string `json:"_echo_key"`
		}
		if ignoreExisting && (event.EventType == "agent.message" || event.EventType == "agent.thinking") && len(event.Payload) > 0 {
			if err := jsonv2.Unmarshal(event.Payload, &echo); err != nil {
				return nil, err
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
					return nil, err
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
		if ignoreExisting {
			_, err := NewSessionEventMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, event.ExternalID)
			if err == nil {
				continue
			}
			if !errors.Is(mapNoRows(err), ErrNotFound) {
				return nil, err
			}
		}
		batch := []SessionEvent{event}
		if event.ExternalID == acceptedID {
			event.ProcessedAt = event.CreatedAt
			running := event
			running.EventType = "session.status_running"
			running.ExternalID = derivedStatusEventID(event.ExternalID, running.EventType)
			running.Payload = nil
			batch, err = sessionStatusEventsTx(ctx, executor, session, primary, worker, running)
			batch = append(batch, event)
		} else if maevents.CategoryFor(event.EventType) == maevents.CategorySessionStatus || maevents.CategoryFor(event.EventType) == maevents.CategoryThreadStatus {
			batch, err = sessionStatusEventsTx(ctx, executor, session, primary, worker, event)
		}
		if err != nil {
			return nil, err
		}
		for _, next := range batch {
			stored, inserted, err := insertSessionEventTx(ctx, executor, &session, primary, next, ignoreExisting)
			if err != nil {
				return nil, err
			}
			if inserted {
				created = append(created, stored)
			}
		}
	}
	if slices.ContainsFunc(created, func(event SessionEvent) bool { return maevents.IsPublicWorkerInputEvent(event.EventType) }) {
		newTurn := slices.ContainsFunc(created, func(event SessionEvent) bool { return event.ExternalID == acceptedID })
		if err := NewCodeSessionMapper(executor).ResetIdleSinceForSession(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.UUID, newTurn); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func idlePrimaryInputID(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker codeSessionInputStateRow, events []SessionEvent) (string, error) {
	if primary.Status != "idle" || worker.WorkerStatus == "requires_action" {
		return "", nil
	}
	first := slices.IndexFunc(events, func(event SessionEvent) bool {
		return event.EventType == "user.message" && event.ProcessedAt.IsZero() &&
			(event.ThreadExternalID == nil || *event.ThreadExternalID == primary.ExternalID)
	})
	if first < 0 {
		return "", nil
	}
	if slices.ContainsFunc(events[:first], func(event SessionEvent) bool {
		return maevents.IsPublicWorkerInputEvent(event.EventType) && event.ProcessedAt.IsZero() &&
			(event.ThreadExternalID == nil || *event.ThreadExternalID == primary.ExternalID)
	}) {
		return "", nil
	}
	pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, primary.ExternalID)
	if err != nil || len(pending) > 0 {
		return "", err
	}
	// Descending history puts every unacknowledged worker input first.
	latest, err := NewSessionEventMapper(executor).ListPage(ctx, sessionEventPageMapperParams{
		WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID,
		ThreadExternalID: primary.ExternalID,
		Descending:       true, FetchLimit: 1,
	})
	if err != nil {
		return "", err
	}
	if len(latest) > 0 && latest[0].ProcessedAt == nil {
		return "", nil
	}
	return events[first].ExternalID, nil
}

func insertSessionEventTx(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, event SessionEvent, ignoreExisting bool) (SessionEvent, bool, error) {
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
	mapper := NewSessionEventMapper(executor)
	var row sessionEventRow
	if ignoreExisting {
		var inserted bool
		row, inserted, err = mapper.InsertIfAbsent(ctx, sessionEventWriteParameters(event))
		if err != nil || !inserted {
			return SessionEvent{}, false, err
		}
	} else {
		row, err = mapper.Insert(ctx, sessionEventWriteParameters(event))
		if err != nil {
			return SessionEvent{}, false, err
		}
	}
	if err := attachEventPayloadBlob(ctx, executor, session.WorkspaceUUID, event.PayloadBlobUUID); err != nil {
		return SessionEvent{}, false, err
	}
	if err := applySessionEventState(ctx, executor, session, primary.ExternalID, event); err != nil {
		return SessionEvent{}, false, err
	}
	return row.event(), true, nil
}
