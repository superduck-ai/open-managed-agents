package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type threadStatusEventPayload struct {
	StopReason json.RawMessage `json:"stop_reason"`
}

type threadIdleReason struct {
	Type     string   `json:"type"`
	EventIDs []string `json:"event_ids,omitempty"`
}

// A worker reports thread facts. Aggregate transitions and new action requests
// emit Session status in the same transaction; retries never reconsider old facts.
func appendThreadStatusEvent(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, codeSessionID string, event db.SessionEvent) ([]db.SessionEvent, error) {
	ignoredTimes := []string{"created_at", "processed_at", "timestamp"}
	if _, err := tx.GetSessionEvent(ctx, session, event.ExternalID); err == nil {
		return tx.AppendSessionEventsIfAbsent(ctx, session, []db.SessionEvent{event}, ignoredTimes)
	} else if !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	current, err := tx.LockSessionForEvents(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	threads, err := tx.ListSessionThreads(ctx, current)
	if err != nil {
		return nil, err
	}
	change := event.StateChange
	if change == nil || change.ThreadExternalID == "" {
		return nil, db.ErrInvalidState
	}
	statuses := make([]string, len(threads))
	for i, thread := range threads {
		statuses[i] = thread.Status
		if thread.ExternalID == change.ThreadExternalID {
			statuses[i] = change.Status
		}
	}
	status := maevents.AggregateThreadStatuses(statuses)
	var source threadStatusEventPayload
	if err := json.Unmarshal(event.Payload, &source); err != nil {
		return nil, err
	}
	var reason threadIdleReason
	if len(source.StopReason) > 0 {
		if err := json.Unmarshal(source.StopReason, &reason); err != nil {
			return nil, err
		}
	}
	if status == current.Status && !(status == "idle" && reason.Type == "requires_action") {
		return tx.AppendSessionEventsIfAbsent(ctx, current, []db.SessionEvent{event}, ignoredTimes)
	}
	eventType := "session.status_" + status
	if status == "rescheduling" {
		eventType = "session.status_rescheduled"
	}
	aggregate := event
	aggregate.UUID = uuid.NewV4().String()
	aggregate.ExternalID = derivedSessionEventID(codeSessionID, event.ExternalID, eventType, "aggregate")
	aggregate.EventType = eventType
	aggregate.ThreadUUID, aggregate.ThreadExternalID, aggregate.StateChange = nil, nil, nil
	payload := map[string]any{"id": aggregate.ExternalID, "type": eventType}
	if status == "idle" {
		worker, found, err := tx.GetSessionCodeSession(ctx, current, codeSessionID)
		if err != nil {
			return nil, err
		}
		var pending []string
		if found {
			pending, err = codesessions.PendingToolActionEventIDs(worker.WorkerExternalMetadata)
			if err != nil {
				return nil, err
			}
		}
		// The current ask is persisted to metadata after these public events,
		// within this same transaction. Include it before that write occurs.
		if reason.Type == "requires_action" {
			pending = append(pending, reason.EventIDs...)
		}
		slices.Sort(pending)
		if len(pending) > 0 {
			payload["stop_reason"] = threadIdleReason{Type: "requires_action", EventIDs: slices.Compact(pending)}
		} else if reason.Type == "" {
			payload["stop_reason"] = threadIdleReason{Type: "end_turn"}
		} else {
			payload["stop_reason"] = source.StopReason
		}
	}
	aggregate.Payload, err = json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	batch := []db.SessionEvent{aggregate, event}
	if status == "idle" || status == "terminated" {
		batch[0], batch[1] = batch[1], batch[0]
	}
	return tx.AppendSessionEventsIfAbsent(ctx, current, batch, ignoredTimes)
}
