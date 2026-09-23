package db

import (
	"context"
	jsonv2 "encoding/json/v2"
	"slices"
	"time"
	"uuid"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/yourbatis"
)

type sessionStopReason struct {
	Type     string   `json:"type"`
	Detail   string   `json:"detail,omitempty"`
	EventIDs []string `json:"event_ids,omitempty"`
}

type sessionStatusPayload struct {
	ID              string             `json:"id"`
	Type            string             `json:"type"`
	CreatedAt       time.Time          `json:"created_at"`
	ProcessedAt     time.Time          `json:"processed_at"`
	SessionThreadID string             `json:"session_thread_id,omitempty"`
	AgentName       string             `json:"agent_name,omitempty"`
	StopReason      *sessionStopReason `json:"stop_reason,omitempty"`
}

func sessionStatusEventsTx(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, worker codeSessionInputStateRow, source SessionEvent) ([]SessionEvent, error) {
	if source.EventType == "session.deleted" {
		return []SessionEvent{source}, nil
	}
	status, isThread := maevents.ThreadStatus(source.EventType)
	if !isThread {
		var isStatus bool
		status, isStatus = maevents.SessionStatus(source.EventType)
		if !isStatus {
			return []SessionEvent{source}, nil
		}
	}
	thread := primary
	if isThread && source.StatusThreadID != "" && source.StatusThreadID != primary.ExternalID {
		row, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, source.StatusThreadID)
		if err != nil {
			return nil, mapNoRows(err)
		}
		thread = row.thread()
	}
	pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, thread.ExternalID)
	if err != nil {
		return nil, err
	}
	if status == "running" && len(pending) > 0 {
		return nil, nil
	}
	var payload sessionStatusPayload
	if len(source.Payload) > 0 {
		if err := jsonv2.Unmarshal(source.Payload, &payload); err != nil {
			return nil, err
		}
	}
	allPending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, primary.ExternalID, "")
	if err != nil {
		return nil, err
	}
	if status == "idle" && payload.StopReason != nil && payload.StopReason.Type == "requires_action" &&
		(len(allPending) == 0 || (isThread && len(pending) == 0)) {
		return nil, nil
	}
	suffix := status
	if suffix == "rescheduling" {
		suffix = "rescheduled"
	}
	threadEvent, err := newSessionStatusEvent(source, "session.thread_status_"+suffix, thread, statusStopReason(status, payload.StopReason, pending))
	if err != nil {
		return nil, err
	}
	sessionStatus := status
	if isThread {
		threads, err := NewSessionThreadMapper(executor).List(ctx, session.WorkspaceUUID, session.ExternalID)
		if err != nil {
			return nil, err
		}
		sessionStatus = sessionStatusAfterThread(threads, thread.ExternalID, status)
	}
	sessionSuffix := sessionStatus
	if sessionSuffix == "rescheduling" {
		sessionSuffix = "rescheduled"
	}
	sessionType := "session.status_" + sessionSuffix
	if !isThread {
		sessionType = source.EventType
	}
	sessionEvent, err := newSessionStatusEvent(source, sessionType, SessionThread{}, statusStopReason(sessionStatus, payload.StopReason, allPending))
	if err != nil {
		return nil, err
	}
	if sessionStatus == "running" || sessionStatus == "rescheduling" {
		return []SessionEvent{sessionEvent, threadEvent}, nil
	}
	return []SessionEvent{threadEvent, sessionEvent}, nil
}

func sessionStatusAfterThread(threads []sessionThreadRow, threadID, status string) string {
	for i := range threads {
		if threads[i].ExternalID == threadID {
			threads[i].Status = status
		}
	}
	for _, active := range []string{"running", "rescheduling", "idle"} {
		if slices.ContainsFunc(threads, func(thread sessionThreadRow) bool { return thread.Status == active }) {
			return active
		}
	}
	return "terminated"
}

func statusStopReason(status string, reason *sessionStopReason, pending []string) *sessionStopReason {
	if status != "idle" {
		return nil
	}
	if len(pending) > 0 {
		return &sessionStopReason{Type: "requires_action", EventIDs: pending}
	}
	if reason == nil || reason.Type == "" || reason.Type == "requires_action" {
		return &sessionStopReason{Type: "end_turn"}
	}
	return reason
}

func newSessionStatusEvent(source SessionEvent, eventType string, thread SessionThread, reason *sessionStopReason) (SessionEvent, error) {
	eventID := source.ExternalID
	if eventType != source.EventType {
		eventID += "_" + eventType
	}
	type statusAgent struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	var agent statusAgent
	if len(thread.AgentSnapshot) > 0 {
		if err := jsonv2.Unmarshal(thread.AgentSnapshot, &agent); err != nil {
			return SessionEvent{}, err
		}
	}
	if agent.Name == "" {
		agent.Name = agent.DisplayName
	}
	payload, err := jsonv2.Marshal(sessionStatusPayload{
		ID: eventID, Type: eventType, CreatedAt: source.CreatedAt, ProcessedAt: source.ProcessedAt,
		SessionThreadID: thread.ExternalID, AgentName: agent.Name, StopReason: reason,
	})
	if err != nil {
		return SessionEvent{}, err
	}
	return SessionEvent{
		UUID: uuid.NewV4().String(), ExternalID: eventID, EventType: eventType,
		StatusThreadID: thread.ExternalID, Payload: payload,
		CreatedAt: source.CreatedAt, ProcessedAt: source.ProcessedAt,
	}, nil
}

func shouldWriteSessionStatus(ctx context.Context, executor yourbatis.Executor, session Session, primary SessionThread, event SessionEvent) (bool, error) {
	if event.EventType == "session.deleted" {
		return true, nil
	}
	status, isThread := maevents.ThreadStatus(event.EventType)
	current, threadID := session.Status, ""
	if isThread {
		threadID = event.StatusThreadID
		if threadID == "" {
			threadID = primary.ExternalID
		}
		row, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, threadID)
		if err != nil {
			return false, mapNoRows(err)
		}
		current = row.Status
	} else {
		var isStatus bool
		status, isStatus = maevents.SessionStatus(event.EventType)
		if !isStatus {
			return true, nil
		}
		if status == "idle" {
			threads, err := NewSessionThreadMapper(executor).List(ctx, session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				return false, err
			}
			if slices.ContainsFunc(threads, func(t sessionThreadRow) bool { return t.Status == "running" || t.Status == "rescheduling" }) {
				return false, nil
			}
		}
	}
	if current != status {
		return true, nil
	}
	if status != "idle" {
		return false, nil
	}
	previous, found, err := NewSessionEventMapper(executor).FindLatestStatus(ctx, session.WorkspaceUUID, session.ExternalID, threadID)
	if err != nil {
		return false, err
	}
	var before, after sessionStatusPayload
	if found {
		if err := jsonv2.Unmarshal(previous.Payload, &before); err != nil {
			return false, err
		}
	}
	if err := jsonv2.Unmarshal(event.Payload, &after); err != nil {
		return false, err
	}
	oldReason, newReason := before.StopReason, after.StopReason
	if oldReason == nil {
		oldReason = &sessionStopReason{Type: "end_turn"}
	}
	if newReason == nil {
		newReason = &sessionStopReason{Type: "end_turn"}
	}
	slices.Sort(oldReason.EventIDs)
	slices.Sort(newReason.EventIDs)
	return oldReason.Type != newReason.Type || oldReason.Detail != newReason.Detail || !slices.Equal(slices.Compact(oldReason.EventIDs), slices.Compact(newReason.EventIDs)), nil
}

// applySessionEventState runs only for newly inserted facts under the session lock.
func applySessionEventState(ctx context.Context, executor yourbatis.Executor, session *Session, primaryID string, event SessionEvent) error {
	mapper := NewSessionMapper(executor)
	if status, ok := maevents.ThreadStatus(event.EventType); ok {
		threadID := event.StatusThreadID
		if threadID == "" {
			threadID = primaryID
		}
		_, err := NewSessionThreadMapper(executor).SetStatus(ctx, session.WorkspaceUUID, session.ExternalID, threadID, status)
		return err
	}
	if status, ok := maevents.SessionStatus(event.EventType); ok {
		if _, err := mapper.SetStatus(ctx, session.WorkspaceUUID, session.ExternalID, status); err != nil {
			return err
		}
		session.Status = status
	}
	return nil
}
