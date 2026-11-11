package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

const (
	maxScoredAnswer     = 16000
	maxScoredTrajectory = 16000
	maxScoredStep       = 1500
	maxMinorStep        = 400
	maxStoredJudgeText  = 64000
	redactedValue       = "[redacted]"
)

var (
	sensitiveKeyPattern = regexp.MustCompile(`(?i)(authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|secret|password|passwd|cookie|credential|private[_-]?key|^token$)`)
	bearerPattern       = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`)
	apiKeyPattern       = regexp.MustCompile(`\b(?:sk|pk|rk)-[A-Za-z0-9_-]{16,}`)
	majorEventTypes     = map[string]bool{
		"user.message": true, "agent.message": true, "agent.tool_use": true, "agent.mcp_tool_use": true,
		"agent.custom_tool_use": true, "agent.tool_result": true, "agent.mcp_tool_result": true,
		"session.error": true, "session.status_idle": true, "session.status_terminated": true,
	}
)

type judgeEvidence struct {
	answer     string
	trajectory string
}

type trajectoryLine struct {
	header  string
	payload string
	minor   bool
}

func (r *TaskRunner) scoreItem(ctx context.Context, task db.EvaluationTask, item db.EvaluationTaskItem, finalAttempt bool) error {
	score, err := newScore(task, item)
	if err != nil {
		return err
	}
	events, err := r.itemEvents(ctx, task.WorkspaceUUID, item.SessionExternalID)
	if errors.Is(err, errSessionTooLarge) {
		score.Status, score.ErrorMessage = scoreFailed, scoreSessionTooLargeMessage
		return r.saveScore(ctx, score)
	}
	if err != nil {
		return err
	}
	evidence := evidenceFromEvents(events)
	score.Answer = evidence.answer
	score.JudgeSystem = scoringSystem(task.RubricBody)
	score.JudgePrompt = scoringPrompt(redactedCase(item), evidence.answer, evidence.trajectory)
	completion, err := r.complete(ctx, task, score.JudgeSystem, score.JudgePrompt)
	score.JudgeResponse = completion.Text
	score.JudgeInputTokens, score.JudgeOutputTokens = completion.InputTokens, completion.OutputTokens
	if err := judgeScore(&score, completion.Text, err, task.PassScore, finalAttempt); err != nil {
		return err
	}
	limitUnansweredScore(&score, task.PassScore)
	return r.saveScore(ctx, score)
}

func (r *TaskRunner) itemEvents(ctx context.Context, workspaceUUID, sessionID string) ([]db.SessionEvent, error) {
	if sessionID == "" {
		return nil, nil
	}
	return loadSessionEvents(ctx, r.store, workspaceUUID, sessionID)
}

func (r *TaskRunner) complete(ctx context.Context, task db.EvaluationTask, system, user string) (Completion, error) {
	if r.model == nil {
		return Completion{}, errModelMissing
	}
	return r.model.Complete(ctx, CompletionRequest{
		OrganizationUUID: task.OrganizationUUID, WorkspaceUUID: task.WorkspaceUUID, Model: task.RubricModel, System: system, User: user,
	})
}

func (r *TaskRunner) saveScore(ctx context.Context, score db.EvaluationTaskScore) error {
	score.JudgeSystem = clipMiddle(score.JudgeSystem, maxStoredJudgeText)
	score.JudgePrompt = clipMiddle(score.JudgePrompt, maxStoredJudgeText)
	score.JudgeResponse = clipMiddle(score.JudgeResponse, maxStoredJudgeText)
	_, err := r.store.CreateEvaluationTaskScore(ctx, score)
	if errors.Is(err, db.ErrDuplicate) {
		return nil
	}
	return err
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

func judgeScore(score *db.EvaluationTaskScore, text string, modelErr error, passScore int, finalAttempt bool) error {
	if modelErr != nil {
		if !finalAttempt && !errors.Is(modelErr, errModelMissing) {
			return modelErr
		}
		score.Status, score.ErrorMessage = scoreFailed, clipError(modelErrorMessage(modelErr))
		return nil
	}
	parsed, err := parseScoreText(text, passScore)
	if err != nil {
		if !finalAttempt {
			return err
		}
		score.Status, score.ErrorMessage = scoreFailed, scoreParseFailedMessage
		return nil
	}
	score.Status, score.Score, score.Passed = scoreCompleted, parsed.Score, parsed.Passed
	score.Explanation, score.Dimensions = parsed.Explanation, parsed.Dimensions
	return nil
}

func limitUnansweredScore(score *db.EvaluationTaskScore, passScore int) {
	if score.Status != scoreCompleted || score.Answer != "" {
		return
	}
	score.Passed = false
	if passScore > 0 && score.Score >= float64(passScore) {
		score.Score = float64(passScore - 1)
	}
}

func modelErrorMessage(err error) string {
	if errors.Is(err, errModelMissing) {
		return modelMissingMessage
	}
	return scoreFailedMessage
}

func evidenceFromEvents(events []db.SessionEvent) judgeEvidence {
	var answers []string
	lines := make([]trajectoryLine, 0, len(events))
	for _, event := range events {
		if event.EventType == "agent.message" {
			if text := eventText(event.Payload); text != "" {
				answers = append(answers, redactText(text))
			}
		}
		lines = append(lines, lineFromEvent(event))
	}
	return judgeEvidence{
		answer:     clipMiddle(strings.Join(answers, "\n"), maxScoredAnswer),
		trajectory: clipMiddle(joinTrajectory(lines), maxScoredTrajectory),
	}
}

func joinTrajectory(lines []trajectoryLine) string {
	full := renderTrajectory(lines, true)
	if len(full) <= maxScoredTrajectory {
		return full
	}
	return renderTrajectory(lines, false)
}

func renderTrajectory(lines []trajectoryLine, keepMinor bool) string {
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		if line.payload == "" || (line.minor && !keepMinor) {
			rendered = append(rendered, line.header)
			continue
		}
		rendered = append(rendered, line.header+" "+line.payload)
	}
	return strings.Join(rendered, "\n")
}

func lineFromEvent(event db.SessionEvent) trajectoryLine {
	minor := !majorEventTypes[event.EventType]
	limit := maxScoredStep
	if minor {
		limit = maxMinorStep
	}
	payload := clipMiddle(redactedPayload(event.Payload), limit)
	if payload == "null" || payload == "{}" {
		payload = ""
	}
	return trajectoryLine{header: formatTime(event.CreatedAt) + " " + event.EventType, payload: payload, minor: minor}
}

func redactedCase(item db.EvaluationTaskItem) caseDraft {
	return caseDraft{
		Query: redactText(item.Query), ExpectedAnswer: redactText(item.ExpectedAnswer),
		ExpectedToolTrace: json.RawMessage(redactedPayload(item.ExpectedToolTrace)), Acceptance: redactText(item.Acceptance),
	}
}

func redactedPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return redactText(string(raw))
	}
	encoded, err := json.Marshal(redactValue(value))
	if err != nil {
		return ""
	}
	return string(encoded)
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if _, isText := nested.(string); isText && sensitiveKeyPattern.MatchString(key) {
				typed[key] = redactedValue
				continue
			}
			typed[key] = redactValue(nested)
		}
		return typed
	case []any:
		for index, nested := range typed {
			typed[index] = redactValue(nested)
		}
		return typed
	case string:
		return redactText(typed)
	default:
		return value
	}
}

func redactText(text string) string {
	text = bearerPattern.ReplaceAllString(text, "Bearer "+redactedValue)
	return apiKeyPattern.ReplaceAllString(text, redactedValue)
}
