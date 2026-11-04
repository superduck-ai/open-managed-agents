package db

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func reduceDSHState(ctx context.Context, executor yourbatis.Executor, session *Session, primary SessionThread, scope DSHScope, seed string) ([]SessionEvent, error) {
	mapper := NewDSHExecutionMapper(executor)
	summary, err := mapper.Summary(ctx, scope)
	if err != nil {
		return nil, err
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
	running, pending := false, make([]string, 0)
	for _, thread := range threads {
		if thread.Status == "terminated" {
			continue
		}
		ids := dshPendingIDs(permissions, thread.ExternalID)
		status := thread.Status
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
		event, err := dshStatusEvent(seed, thread.thread(), status, reason, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		written, err := insertDSHHistory(ctx, executor, session, primary, []SessionEvent{event})
		if err != nil {
			return nil, err
		}
		events = append(events, written...)
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
	event, err := dshStatusEvent(seed, SessionThread{}, status, reason, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if status == "idle" && session.Status != "idle" {
		usage := SessionEvent{ExternalID: derivedStatusEventID(event.ExternalID, "session.usage"), EventType: "session.usage", CreatedAt: event.CreatedAt, ProcessedAt: event.ProcessedAt}
		written, err := insertDSHHistory(ctx, executor, session, primary, []SessionEvent{usage})
		if err != nil {
			return nil, err
		}
		events = append(events, written...)
	}
	written, err := insertDSHHistory(ctx, executor, session, primary, []SessionEvent{event})
	return append(events, written...), err
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
