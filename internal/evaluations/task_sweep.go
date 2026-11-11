package evaluations

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	sweepLimit    = 100
	staleItemAge  = 2 * time.Minute
	settleTaskAge = time.Minute
	startGrace    = 10 * time.Minute
)

type sessionOutcome struct {
	status   string
	message  string
	terminal bool
}

func (r *TaskRunner) sweep(ctx context.Context, now time.Time) error {
	tasks := settleRequests{}
	if err := r.abandonUnstarted(ctx, now, &tasks); err != nil {
		return err
	}
	items, err := r.store.ListStaleRunningEvaluationItems(ctx, now.Add(-staleItemAge), sweepLimit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if r.reconcileItem(ctx, item, now) {
			tasks.add(item.OrganizationUUID, item.WorkspaceUUID, item.TaskExternalID)
		}
	}
	refs, err := r.store.ListSettleableEvaluationTasks(ctx, now.Add(-settleTaskAge), sweepLimit)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		tasks.add(ref.OrganizationUUID, ref.WorkspaceUUID, ref.ExternalID)
	}
	r.settleAll(ctx, tasks)
	return nil
}

func (r *TaskRunner) abandonUnstarted(ctx context.Context, now time.Time, tasks *settleRequests) error {
	items, err := r.store.ListUnstartedEvaluationItems(ctx, now.Add(-startGrace), sweepLimit)
	if err != nil {
		return err
	}
	for _, item := range items {
		abandoned, err := r.store.AbandonEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, startInterruptedMessage, now)
		if err != nil {
			r.logger.WarnContext(ctx, "abandon evaluation item", "item_id", item.ExternalID, "error", err)
			continue
		}
		if abandoned {
			tasks.add(item.OrganizationUUID, item.WorkspaceUUID, item.TaskExternalID)
		}
	}
	return nil
}

func (r *TaskRunner) reconcileItem(ctx context.Context, item db.EvaluationTaskItem, now time.Time) bool {
	outcome, err := r.sessionOutcome(ctx, item)
	if err != nil {
		r.logger.WarnContext(ctx, "reconcile evaluation item", "item_id", item.ExternalID, "session_id", item.SessionExternalID, "error", err)
		return false
	}
	if outcome.terminal {
		return r.finishItem(ctx, item, outcome)
	}
	if err := r.store.TouchRunningEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, now); err != nil {
		r.logger.WarnContext(ctx, "touch evaluation item", "item_id", item.ExternalID, "error", err)
	}
	return false
}

func (r *TaskRunner) sessionOutcome(ctx context.Context, item db.EvaluationTaskItem) (sessionOutcome, error) {
	session, ok, err := r.store.GetSession(ctx, item.WorkspaceUUID, item.SessionExternalID)
	if err != nil {
		return sessionOutcome{}, err
	}
	if !ok || session.Status == "terminated" || session.ArchivedAt != nil || session.DeletedAt != nil {
		return terminatedOutcome(), nil
	}
	if session.Status != "idle" {
		return sessionOutcome{}, nil
	}
	events, _, err := r.store.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceUUID: item.WorkspaceUUID, SessionExternalID: item.SessionExternalID, Types: []string{"session.status_idle"}, Order: "desc", Limit: 1,
	})
	if err != nil {
		return sessionOutcome{}, err
	}
	return terminalStatus(events), nil
}

func terminalStatus(events []db.SessionEvent) sessionOutcome {
	failed := false
	completed := false
	for _, event := range events {
		switch event.EventType {
		case "session.status_terminated", "session.deleted":
			failed = true
		case "session.status_idle":
			switch stopReason(event.Payload) {
			case "retries_exhausted":
				failed = true
			case "end_turn", "requires_action":
				completed = true
			}
		}
	}
	if failed {
		return terminatedOutcome()
	}
	if completed {
		return sessionOutcome{status: itemCompleted, terminal: true}
	}
	return sessionOutcome{}
}

func terminatedOutcome() sessionOutcome {
	return sessionOutcome{status: itemFailed, message: sessionTerminatedMessage, terminal: true}
}

func stopReason(payload json.RawMessage) string {
	var body struct {
		StopReason *struct {
			Type string `json:"type"`
		} `json:"stop_reason"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.StopReason == nil {
		return ""
	}
	return body.StopReason.Type
}
