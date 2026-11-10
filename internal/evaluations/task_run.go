package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

func (h *Handler) ObserveSessionEvents(ctx context.Context, events []db.SessionEvent) {
	if h == nil || len(events) == 0 {
		return
	}
	copied := append([]db.SessionEvent(nil), events...)
	if h.observeSync {
		h.consumeSessionEvents(ctx, copied)
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				h.logger.ErrorContext(detached, "evaluation observer panicked", "error", fmt.Sprint(recovered))
			}
		}()
		h.consumeSessionEvents(detached, copied)
	}()
}

func (h *Handler) consumeSessionEvents(ctx context.Context, events []db.SessionEvent) {
	grouped := map[string][]db.SessionEvent{}
	var keys []string
	for _, event := range events {
		if event.OrganizationUUID == "" || event.WorkspaceUUID == "" || event.SessionExternalID == "" {
			continue
		}
		key := event.OrganizationUUID + "\x00" + event.WorkspaceUUID + "\x00" + event.SessionExternalID
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], event)
	}
	tasks := map[string]db.EvaluationTaskItem{}
	for _, key := range keys {
		batch := grouped[key]
		status, message, terminal := terminalStatus(batch)
		if !terminal {
			continue
		}
		first := batch[0]
		items, err := h.store.ListRunningEvaluationItemsBySession(ctx, first.OrganizationUUID, first.WorkspaceUUID, first.SessionExternalID)
		if err != nil {
			h.logger.ErrorContext(ctx, "list evaluation items for session", "session_id", first.SessionExternalID, "error", err)
			continue
		}
		for _, item := range items {
			finished, err := h.store.FinishRunningEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, status, clipError(message), time.Now().UTC())
			if err != nil {
				h.logger.ErrorContext(ctx, "finish evaluation item", "item_id", item.ExternalID, "error", err)
				continue
			}
			if finished {
				tasks[item.TaskExternalID] = item
			}
		}
	}
	for _, item := range tasks {
		h.settleTask(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.TaskExternalID)
	}
}

func (h *Handler) settleTask(ctx context.Context, organizationUUID, workspaceUUID, taskID string) {
	detail, err := h.store.GetEvaluationTask(ctx, organizationUUID, workspaceUUID, taskID)
	if err != nil || detail.Task.Status == "cancelled" || detail.Task.Status == "completed" || detail.Task.Status == "failed" {
		return
	}
	round := latestRound(detail.Items)
	if !roundSettled(detail.Items, round) {
		return
	}
	if err := h.scoreRound(ctx, detail, round); err != nil {
		h.logger.ErrorContext(ctx, "score evaluation round", "task_id", taskID, "error", err)
	}
	detail, err = h.store.GetEvaluationTask(ctx, organizationUUID, workspaceUUID, taskID)
	if err != nil {
		return
	}
	status := "completed"
	if allTerminalFailed(itemsForRound(detail.Items, round)) {
		status = "failed"
	}
	if err := h.store.UpdateEvaluationTaskStatus(ctx, organizationUUID, workspaceUUID, taskID, status, "", time.Now().UTC()); err != nil {
		h.logger.ErrorContext(ctx, "update evaluation task status", "task_id", taskID, "error", err)
	}
}

func (h *Handler) scoreRound(ctx context.Context, detail db.EvaluationTaskDetail, round int) error {
	scored := map[string]struct{}{}
	for _, score := range detail.Scores {
		scored[score.ItemUUID] = struct{}{}
	}
	for _, item := range detail.Items {
		if item.Round != round || item.Status != "completed" {
			continue
		}
		if _, ok := scored[item.UUID]; ok {
			continue
		}
		if err := h.scoreItem(ctx, detail.Task, item); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) scoreItem(ctx context.Context, task db.EvaluationTask, item db.EvaluationTaskItem) error {
	output, trajectory := "", ""
	if item.SessionExternalID != "" {
		events, err := h.sessionEvents(ctx, task.WorkspaceUUID, item.SessionExternalID)
		if err != nil {
			return err
		}
		output, trajectory = transcriptFromEvents(events)
	}
	prompt := scoringPrompt(task.RubricBody, caseDraft{
		Query: item.Query, ExpectedAnswer: item.ExpectedAnswer, ExpectedToolTrace: item.ExpectedToolTrace, Acceptance: item.Acceptance,
	}, output, trajectory)
	text, err := h.complete(ctx, task.OrganizationUUID, task.WorkspaceUUID, task.RubricModel, rubricSystemPrompt, prompt)
	score := db.EvaluationTaskScore{
		UUID: uuid.NewString(), OrganizationUUID: task.OrganizationUUID, WorkspaceUUID: task.WorkspaceUUID,
		TaskUUID: task.UUID, ItemUUID: item.UUID, CreatedAt: time.Now().UTC(), Dimensions: json.RawMessage(`[]`),
	}
	externalID, idErr := ids.New("evalscore_")
	if idErr != nil {
		return idErr
	}
	score.ExternalID = externalID
	if err != nil {
		score.Status = "failed"
		score.ErrorMessage = clipError(modelErrorMessage(err))
	} else {
		parsed, parseErr := parseScoreText(text, task.PassScore)
		if parseErr != nil {
			score.Status = "failed"
			score.ErrorMessage = "Could not parse evaluator score"
		} else {
			score.Status = "completed"
			score.Score = parsed.Score
			score.Passed = parsed.Passed
			score.Explanation = parsed.Explanation
			score.Dimensions = parsed.Dimensions
		}
	}
	_, err = h.store.CreateEvaluationTaskScore(ctx, score)
	if errors.Is(err, db.ErrDuplicate) {
		return nil
	}
	return err
}

func (h *Handler) startItems(ctx context.Context, principal auth.Principal, task db.EvaluationTask, items []db.EvaluationTaskItem) {
	const prefix = "评测 · "
	for _, item := range items {
		if item.Status != "pending" && item.Status != "" {
			continue
		}
		now := time.Now().UTC()
		if h.starter == nil {
			_, _ = h.store.MarkEvaluationItemRunning(ctx, task.OrganizationUUID, task.WorkspaceUUID, item.UUID, "", now)
			_, _ = h.store.FinishRunningEvaluationItem(ctx, task.OrganizationUUID, task.WorkspaceUUID, item.UUID, "failed", starterMissingMessage, now)
			continue
		}
		sessionID, err := h.starter.Start(ctx, SessionInput{
			Principal: principal, AgentExternalID: task.AgentExternalID, AgentVersion: task.AgentVersion,
			EnvironmentExternalID: task.EnvironmentExternalID, Resources: task.Resources, VaultIDs: vaultList(task.VaultIDs),
			Query: item.Query, Title: clip(prefix+item.Query, 120),
			Metadata: map[string]string{"evaluation_task_id": task.ExternalID, "evaluation_item_id": item.ExternalID},
		})
		if err != nil {
			if item.Status == "" {
				_, _ = h.store.FinishRunningEvaluationItem(ctx, task.OrganizationUUID, task.WorkspaceUUID, item.UUID, "failed", clipError(err.Error()), now)
				continue
			}
			_, _ = h.store.MarkEvaluationItemRunning(ctx, task.OrganizationUUID, task.WorkspaceUUID, item.UUID, "", now)
			_, _ = h.store.FinishRunningEvaluationItem(ctx, task.OrganizationUUID, task.WorkspaceUUID, item.UUID, "failed", clipError(err.Error()), now)
			continue
		}
		if _, err := h.store.MarkEvaluationItemRunning(ctx, task.OrganizationUUID, task.WorkspaceUUID, item.UUID, sessionID, now); err != nil {
			h.logger.ErrorContext(ctx, "mark evaluation item running", "item_id", item.ExternalID, "error", err)
		}
	}
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

func transcriptFromEvents(events []db.SessionEvent) (string, string) {
	var answers []string
	var steps []string
	for _, event := range events {
		text := eventText(event.Payload)
		switch event.EventType {
		case "agent.message":
			if text != "" {
				answers = append(answers, text)
			}
		case "agent.tool_use", "agent.tool_result":
			if text != "" {
				steps = append(steps, event.EventType+": "+text)
			}
		}
	}
	return clip(strings.Join(answers, "\n"), 8000), clip(strings.Join(steps, "\n"), 8000)
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func terminalStatus(events []db.SessionEvent) (string, string, bool) {
	failed := false
	completed := false
	for _, event := range events {
		switch event.EventType {
		case "session.status_terminated", "session.deleted":
			failed = true
		case "session.status_idle":
			reason := stopReason(event.Payload)
			if reason == "retries_exhausted" {
				failed = true
			}
			if reason == "end_turn" {
				completed = true
			}
		}
	}
	if failed {
		return "failed", "Session terminated", true
	}
	if completed {
		return "completed", "", true
	}
	return "", "", false
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

func modelErrorMessage(err error) string {
	if errors.Is(err, errModelMissing) {
		return modelMissingMessage
	}
	return scoreFailedMessage
}
