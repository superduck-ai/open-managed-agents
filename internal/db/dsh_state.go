package db

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func reduceDSHState(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, scope DSHScope, seed string) ([]SessionEvent, error) {
	return reduceDSHStateWithBoundary(ctx, executor, session, primary, scope, seed, false)
}

func reduceDSHStateWithBoundary(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, scope DSHScope, seed string, completed bool) ([]SessionEvent, error) {
	mapper := NewDSHExecutionMapper(executor)
	summary, err := mapper.Summary(ctx, scope)
	if err != nil {
		return nil, err
	}
	if completed {
		summary.HasEligibleAccepted = false
	}
	permissions, err := mapper.Permissions(ctx, scope)
	if err != nil {
		return nil, err
	}
	threads, err := NewSessionThreadMapper(executor).List(ctx, scope.WorkspaceUUID, scope.SessionID)
	if err != nil {
		return nil, err
	}
	var events []SessionEvent
	at := time.Now().UTC().Truncate(time.Microsecond)
	running, pending := false, make([]string, 0)
	for _, thread := range threads {
		if thread.Status == "terminated" {
			continue
		}
		ids := dshPendingIDs(permissions, thread.ExternalID)
		status := thread.Status
		if thread.ExternalID != primary.ExternalID && status == "idle" && len(ids) == 0 && summary.ActiveInputID != nil {
			paused, err := dshThreadWasAwaitingPermission(ctx, executor, *session, thread.ExternalID)
			if err != nil {
				return nil, err
			}
			if paused {
				status = "running"
			}
		}
		reason := &sessionStopReason{Type: "end_turn"}
		if len(ids) > 0 {
			status = "idle"
			reason = &sessionStopReason{Type: "requires_action", EventIDs: ids}
		}
		if thread.ExternalID == primary.ExternalID {
			status, reason = dshPrimaryState(summary, permissions, session.BudgetReachedAt != nil)
		}
		running = running || status == "running" || status == "rescheduling"
		pending = append(pending, ids...)
		if thread.ExternalID != primary.ExternalID && status == thread.Status && len(ids) == 0 {
			continue
		}
		event, err := dshStatusEvent(seed, thread.thread(), status, reason, at)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	status, reason := "idle", &sessionStopReason{Type: "end_turn"}
	if running {
		status = "running"
	} else if len(pending) > 0 {
		slices.Sort(pending)
		reason = &sessionStopReason{Type: "requires_action", EventIDs: slices.Compact(pending)}
	} else {
		_, reason = dshPrimaryState(summary, permissions, session.BudgetReachedAt != nil)
	}
	event, err := dshStatusEvent(seed, SessionThread{}, status, reason, at)
	if err != nil {
		return nil, err
	}
	if status == "running" {
		return insertDSHHistory(ctx, executor, session, primary, append([]SessionEvent{event}, events...))
	}
	if status == "idle" && session.Status != "idle" {
		usage := SessionEvent{ExternalID: derivedStatusEventID(event.ExternalID, "session.usage"), EventType: "session.usage", CreatedAt: event.CreatedAt, ProcessedAt: event.ProcessedAt}
		events = append(events, usage)
	}
	return insertDSHHistory(ctx, executor, session, primary, append(events, event))
}

func dshPendingIDs(permissions []dshPermissionRow, threadID string) []string {
	ids := make([]string, 0)
	for _, permission := range permissions {
		if permission.State == "pending" && (threadID == "" || threadID == permission.ThreadID) {
			ids = append(ids, permission.PublicEventID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func dshPrimaryState(summary dshExecutionSummary, permissions []dshPermissionRow, budget bool) (string, *sessionStopReason) {
	pending := dshPendingIDs(permissions, "")
	if len(pending) > 0 {
		return "idle", &sessionStopReason{Type: "requires_action", EventIDs: pending}
	}
	if budget {
		return "idle", &sessionStopReason{Type: "budget_reached"}
	}
	if summary.ActiveInputID != nil || summary.HasEligibleAccepted {
		return "running", nil
	}
	if summary.LastResult != nil && *summary.LastResult == "failed" {
		return "idle", &sessionStopReason{Type: "retries_exhausted"}
	}
	return "idle", &sessionStopReason{Type: "end_turn"}
}

func dshStatusEvent(seed string, thread SessionThread, status string, reason *sessionStopReason, at time.Time) (SessionEvent, error) {
	kind := "session.status_" + statusEventSuffix(status)
	if thread.ExternalID != "" {
		kind = "session.thread_status_" + statusEventSuffix(status)
	}
	raw, _ := json.Marshal(reason)
	source := SessionEvent{ExternalID: derivedStatusEventID(seed, thread.ExternalID+kind+string(raw)), CreatedAt: at, ProcessedAt: at}
	return newSessionStatusEvent(source, kind, thread, reason)
}

func dshThreadWasAwaitingPermission(ctx context.Context, executor yourbatis.Executor, session Session, threadID string) (bool, error) {
	latest, found, err := NewSessionEventMapper(executor).FindLatestStatus(ctx, session.WorkspaceUUID, session.ExternalID, threadID)
	if err != nil || !found {
		return false, err
	}
	var payload sessionStatusPayload
	if err := json.Unmarshal(latest.Payload, &payload); err != nil {
		return false, err
	}
	return payload.StopReason != nil && payload.StopReason.Type == "requires_action", nil
}

func insertDSHStatusMirror(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, event SessionEvent) ([]SessionEvent, error) {
	if event.StatusThreadID == "" || event.StatusThreadID == primary.ExternalID {
		return nil, nil
	}
	switch event.EventType {
	case "session.thread_status_running", "session.thread_status_idle", "session.thread_status_rescheduled", "session.thread_status_terminated":
	default:
		return nil, nil
	}
	target, err := NewSessionThreadMapper(executor).FindByExternalID(ctx, session.WorkspaceUUID, session.ExternalID, event.StatusThreadID)
	if err != nil {
		return nil, err
	}
	copies, err := sessionThreadStatusCopies(event, primary, target.thread())
	if err != nil {
		return nil, err
	}
	write, err := insertSessionEventIfAbsentTx(ctx, executor, session, primary, copies[1])
	if err != nil || !write.Inserted {
		return nil, err
	}
	return []SessionEvent{write.Event}, nil
}
