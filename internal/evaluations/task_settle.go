package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const settleBatchSize = 5

func (r *TaskRunner) ObserveSessionEvents(ctx context.Context, events []db.SessionEvent) {
	if r == nil || len(events) == 0 {
		return
	}
	copied := slices.Clone(events)
	r.runDetached(ctx, "evaluation observer panicked", func(ctx context.Context) { r.consumeSessionEvents(ctx, copied) })
}

func (r *TaskRunner) consumeSessionEvents(ctx context.Context, events []db.SessionEvent) {
	grouped := map[sessionEventKey][]db.SessionEvent{}
	var keys []sessionEventKey
	for _, event := range events {
		if event.OrganizationUUID == "" || event.WorkspaceUUID == "" || event.SessionExternalID == "" {
			continue
		}
		key := sessionEventKey{organizationUUID: event.OrganizationUUID, workspaceUUID: event.WorkspaceUUID, sessionID: event.SessionExternalID}
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], event)
	}
	tasks := settleRequests{}
	for _, key := range keys {
		batch := grouped[key]
		outcome := terminalStatus(batch)
		if !outcome.terminal {
			continue
		}
		first := batch[0]
		items, err := r.store.ListRunningEvaluationItemsBySession(ctx, first.OrganizationUUID, first.WorkspaceUUID, first.SessionExternalID)
		if err != nil {
			r.logger.ErrorContext(ctx, "list evaluation items for session", "session_id", first.SessionExternalID, "error", err)
			continue
		}
		for _, item := range items {
			if r.finishItem(ctx, item, outcome) {
				tasks.add(item.OrganizationUUID, item.WorkspaceUUID, item.TaskExternalID)
			}
		}
	}
	r.settleAll(ctx, tasks)
}

func (r *TaskRunner) finishItem(ctx context.Context, item db.EvaluationTaskItem, outcome sessionOutcome) bool {
	finished, err := r.store.FinishRunningEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, outcome.status, clipError(outcome.message), time.Now().UTC())
	if err != nil {
		r.logger.ErrorContext(ctx, "finish evaluation item", "item_id", item.ExternalID, "error", err)
		return false
	}
	return finished
}

type sessionEventKey struct {
	organizationUUID string
	workspaceUUID    string
	sessionID        string
}

type settleRequests []db.EvaluationTaskRef

func (s *settleRequests) add(organizationUUID, workspaceUUID, taskID string) {
	for _, existing := range *s {
		if existing.OrganizationUUID == organizationUUID && existing.WorkspaceUUID == workspaceUUID && existing.ExternalID == taskID {
			return
		}
	}
	*s = append(*s, db.EvaluationTaskRef{OrganizationUUID: organizationUUID, WorkspaceUUID: workspaceUUID, ExternalID: taskID})
}

func (r *TaskRunner) settleAll(ctx context.Context, tasks settleRequests) {
	for _, task := range tasks {
		r.requestSettle(ctx, task.OrganizationUUID, task.WorkspaceUUID, task.ExternalID)
	}
}

func (r *TaskRunner) requestSettle(ctx context.Context, organizationUUID, workspaceUUID, taskID string) {
	if r.queue != nil {
		if err := r.queue.EnqueueSettle(ctx, organizationUUID, workspaceUUID, taskID); err != nil {
			r.logger.ErrorContext(ctx, "enqueue evaluation settle", "task_id", taskID, "error", err)
		}
		return
	}
	for {
		done, err := r.settleTask(ctx, organizationUUID, workspaceUUID, taskID, true)
		if err != nil {
			r.logger.ErrorContext(ctx, "settle evaluation task", "task_id", taskID, "error", err)
			return
		}
		if done {
			return
		}
	}
}

func (r *TaskRunner) settleTask(ctx context.Context, organizationUUID, workspaceUUID, taskID string, finalAttempt bool) (bool, error) {
	detail, err := r.store.GetEvaluationTask(ctx, organizationUUID, workspaceUUID, taskID)
	if errors.Is(err, db.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if taskClosed(detail.Task.Status) {
		return true, nil
	}
	round := latestRound(detail.Items)
	if !roundSettled(detail.Items, round) {
		return true, nil
	}
	pending := unscoredItems(detail, round)
	for _, item := range pending[:min(len(pending), settleBatchSize)] {
		if err := r.scoreItem(ctx, detail.Task, item, finalAttempt); err != nil {
			return false, err
		}
	}
	if len(pending) > settleBatchSize {
		return false, nil
	}
	return r.store.FinishEvaluationTask(ctx, organizationUUID, workspaceUUID, taskID, roundStatus(itemsForRound(detail.Items, round)), time.Now().UTC())
}

func unscoredItems(detail db.EvaluationTaskDetail, round int) []db.EvaluationTaskItem {
	scored := map[string]struct{}{}
	for _, score := range detail.Scores {
		scored[score.ItemUUID] = struct{}{}
	}
	pending := make([]db.EvaluationTaskItem, 0)
	for _, item := range detail.Items {
		if item.Round != round || item.Status != itemCompleted {
			continue
		}
		if _, ok := scored[item.UUID]; !ok {
			pending = append(pending, item)
		}
	}
	return pending
}

func itemsForRound(items []db.EvaluationTaskItem, round int) []db.EvaluationTaskItem {
	selected := make([]db.EvaluationTaskItem, 0)
	for _, item := range items {
		if item.Round == round {
			selected = append(selected, item)
		}
	}
	return selected
}

func vaultList(raw json.RawMessage) []string {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func clipError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}

func clipMiddle(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	half := limit / 2
	marker := fmt.Sprintf("\n…[%d bytes omitted]…\n", len(value)-2*half)
	return strings.ToValidUTF8(value[:half], "") + marker + strings.ToValidUTF8(value[len(value)-half:], "")
}
