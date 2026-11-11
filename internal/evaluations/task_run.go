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

const (
	settleBatchSize     = 5
	maxScoredAnswer     = 16000
	maxScoredTrajectory = 16000
	maxScoredStep       = 1500
)

type SettleQueue interface {
	EnqueueSettle(ctx context.Context, organizationUUID, workspaceUUID, taskID string) error
}

func (h *Handler) WithSettleQueue(queue SettleQueue) *Handler {
	h.settleQueue = queue
	return h
}

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
			if h.finishItem(ctx, item, status, message) {
				tasks[item.TaskExternalID] = item
			}
		}
	}
	for _, item := range tasks {
		h.requestSettle(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.TaskExternalID)
	}
}

func (h *Handler) finishItem(ctx context.Context, item db.EvaluationTaskItem, status, message string) bool {
	finished, err := h.store.FinishRunningEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, status, clipError(message), time.Now().UTC())
	if err != nil {
		h.logger.ErrorContext(ctx, "finish evaluation item", "item_id", item.ExternalID, "error", err)
		return false
	}
	return finished
}

func (h *Handler) requestSettle(ctx context.Context, organizationUUID, workspaceUUID, taskID string) {
	if h.settleQueue != nil {
		if err := h.settleQueue.EnqueueSettle(ctx, organizationUUID, workspaceUUID, taskID); err != nil {
			h.logger.ErrorContext(ctx, "enqueue evaluation settle", "task_id", taskID, "error", err)
		}
		return
	}
	for {
		done, err := h.settleTask(ctx, organizationUUID, workspaceUUID, taskID, true)
		if err != nil {
			h.logger.ErrorContext(ctx, "settle evaluation task", "task_id", taskID, "error", err)
			return
		}
		if done {
			return
		}
	}
}

func (h *Handler) settleTask(ctx context.Context, organizationUUID, workspaceUUID, taskID string, finalAttempt bool) (bool, error) {
	detail, err := h.store.GetEvaluationTask(ctx, organizationUUID, workspaceUUID, taskID)
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
		if err := h.scoreItem(ctx, detail.Task, item, finalAttempt); err != nil {
			return false, err
		}
	}
	if len(pending) > settleBatchSize {
		return false, nil
	}
	status := "completed"
	if allTerminalFailed(itemsForRound(detail.Items, round)) {
		status = "failed"
	}
	return true, h.store.UpdateEvaluationTaskStatus(ctx, organizationUUID, workspaceUUID, taskID, status, "", time.Now().UTC())
}

func taskClosed(status string) bool {
	return status == "cancelled" || status == "completed" || status == "failed"
}

func unscoredItems(detail db.EvaluationTaskDetail, round int) []db.EvaluationTaskItem {
	scored := map[string]struct{}{}
	for _, score := range detail.Scores {
		scored[score.ItemUUID] = struct{}{}
	}
	pending := make([]db.EvaluationTaskItem, 0)
	for _, item := range detail.Items {
		if item.Round != round || item.Status != "completed" {
			continue
		}
		if _, ok := scored[item.UUID]; !ok {
			pending = append(pending, item)
		}
	}
	return pending
}

func (h *Handler) scoreItem(ctx context.Context, task db.EvaluationTask, item db.EvaluationTaskItem, finalAttempt bool) error {
	score, err := newScore(task, item)
	if err != nil {
		return err
	}
	output, trajectory, err := h.scoredTranscript(ctx, task.WorkspaceUUID, item.SessionExternalID)
	if errors.Is(err, errSessionTooLarge) {
		score.Status, score.ErrorMessage = "failed", scoreSessionTooLargeMessage
		return h.saveScore(ctx, score)
	}
	if err != nil {
		return err
	}
	score.Answer = output
	score.JudgeSystem = scoringSystem(task.RubricBody)
	score.JudgePrompt = scoringPrompt(caseDraft{
		Query: item.Query, ExpectedAnswer: item.ExpectedAnswer, ExpectedToolTrace: item.ExpectedToolTrace, Acceptance: item.Acceptance,
	}, output, trajectory)
	text, err := h.complete(ctx, task.OrganizationUUID, task.WorkspaceUUID, task.RubricModel, score.JudgeSystem, score.JudgePrompt)
	score.JudgeResponse = text
	if err := judgeScore(&score, text, err, task.PassScore, finalAttempt); err != nil {
		return err
	}
	limitUnansweredScore(&score, task.PassScore)
	return h.saveScore(ctx, score)
}

func newScore(task db.EvaluationTask, item db.EvaluationTaskItem) (db.EvaluationTaskScore, error) {
	externalID, err := ids.New("evalscore_")
	if err != nil {
		return db.EvaluationTaskScore{}, err
	}
	return db.EvaluationTaskScore{
		UUID: uuid.NewString(), ExternalID: externalID, OrganizationUUID: task.OrganizationUUID, WorkspaceUUID: task.WorkspaceUUID,
		TaskUUID: task.UUID, ItemUUID: item.UUID, Dimensions: json.RawMessage(`[]`), JudgeModel: task.RubricModel,
		JudgePromptVersion: scoringPromptVersion, CreatedAt: time.Now().UTC(),
	}, nil
}

func limitUnansweredScore(score *db.EvaluationTaskScore, passScore int) {
	if score.Status != "completed" || score.Answer != "" {
		return
	}
	score.Passed = false
	if passScore > 0 && score.Score >= float64(passScore) {
		score.Score = float64(passScore - 1)
	}
}

func judgeScore(score *db.EvaluationTaskScore, text string, modelErr error, passScore int, finalAttempt bool) error {
	if modelErr != nil {
		if !finalAttempt && !errors.Is(modelErr, errModelMissing) {
			return modelErr
		}
		score.Status, score.ErrorMessage = "failed", clipError(modelErrorMessage(modelErr))
		return nil
	}
	parsed, err := parseScoreText(text, passScore)
	if err != nil {
		if !finalAttempt {
			return err
		}
		score.Status, score.ErrorMessage = "failed", scoreParseFailedMessage
		return nil
	}
	score.Status, score.Score, score.Passed = "completed", parsed.Score, parsed.Passed
	score.Explanation, score.Dimensions = parsed.Explanation, parsed.Dimensions
	return nil
}

func (h *Handler) saveScore(ctx context.Context, score db.EvaluationTaskScore) error {
	_, err := h.store.CreateEvaluationTaskScore(ctx, score)
	if errors.Is(err, db.ErrDuplicate) {
		return nil
	}
	return err
}

func (h *Handler) scoredTranscript(ctx context.Context, workspaceUUID, sessionID string) (string, string, error) {
	if sessionID == "" {
		return "", "", nil
	}
	events, err := h.sessionEvents(ctx, workspaceUUID, sessionID)
	if err != nil {
		return "", "", err
	}
	output, trajectory := transcriptFromEvents(events)
	return output, trajectory, nil
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
			Query: item.Query, Content: item.Inputs, Title: clip(prefix+item.Query, 120),
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
		if event.EventType == "agent.message" {
			if text := eventText(event.Payload); text != "" {
				answers = append(answers, text)
			}
		}
		steps = append(steps, eventLine(event))
	}
	return clipMiddle(strings.Join(answers, "\n"), maxScoredAnswer), clipMiddle(strings.Join(steps, "\n"), maxScoredTrajectory)
}

func eventLine(event db.SessionEvent) string {
	line := formatTime(event.CreatedAt) + " " + event.EventType
	payload := clipMiddle(compactPayload(event.Payload), maxScoredStep)
	if payload == "" || payload == "null" || payload == "{}" {
		return line
	}
	return line + " " + payload
}

func compactPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
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
			if reason == "end_turn" || reason == "requires_action" {
				completed = true
			}
		}
	}
	if failed {
		return "failed", sessionTerminatedMessage, true
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
