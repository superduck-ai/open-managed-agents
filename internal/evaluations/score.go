package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

const (
	agentScoringUnavailableMessage  = "Agent evaluator scoring is not executed"
	evaluatorModelMissingMessage    = "Evaluator model is not configured"
	evaluatorScoreFailedMessage     = "Could not score evaluation"
	evaluatorScoreParseMessage      = "Could not parse evaluator score"
	evaluatorVersionNotFoundMessage = "Evaluation evaluator version not found"
	maxScoreExplanationRunes        = 4000
	maxScoreErrorRunes              = 1024
	maxScoreVariableRunes           = 8000
	maxScoreEventPages              = 5
	scoreEventPageSize              = 100
)

type scoreValues struct {
	Input            string
	Output           string
	Checkpoints      string
	AgentTrajectory  string
	InvocationResult string
}

func (h *Handler) scoreCompletedItem(ctx context.Context, outcome sessionOutcome, match db.EvaluationExperimentSessionMatch) {
	experiment, err := h.store.GetEvaluationExperiment(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, match.ExperimentExternalID)
	if err != nil {
		h.logger.WarnContext(ctx, "load evaluation experiment for scoring",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", match.ExperimentExternalID,
			"error", err,
		)
		return
	}
	values, err := h.loadScoreValues(ctx, outcome, experiment, match.Item)
	if err != nil {
		h.logger.WarnContext(ctx, "load evaluation score values",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", experiment.ExternalID,
			"session_id", outcome.SessionExternalID,
			"error", err,
		)
		values = scoreValues{}
	}
	for _, binding := range experiment.Bindings {
		h.scoreBinding(ctx, outcome, experiment, match.Item, binding, values)
	}
}

func (h *Handler) scoreBinding(ctx context.Context, outcome sessionOutcome, experiment db.EvaluationExperiment, item db.EvaluationExperimentItem, binding db.EvaluationExperimentBinding, values scoreValues) {
	version, err := h.store.GetEvaluationEvaluatorVersion(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, binding.VersionExternalID)
	if err != nil {
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, evaluatorVersionNotFoundMessage)
		return
	}
	if version.EvaluatorType == evaluatorTypeAgent {
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, agentScoringUnavailableMessage)
		return
	}
	if h.model == nil {
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, evaluatorModelMissingMessage)
		return
	}
	scored, scoreErr := h.model.Score(ctx, EvaluatorScoreRequest{
		OrganizationUUID: outcome.OrganizationUUID,
		WorkspaceUUID:    outcome.WorkspaceUUID,
		Model:            version.Model,
		Prompt:           renderEvaluatorPrompt(version.PromptTemplate, version.Variables, values),
	})
	if scoreErr != nil {
		h.logger.WarnContext(ctx, "score evaluation item",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", experiment.ExternalID,
			"evaluator_version_id", version.ExternalID,
			"error", scoreErr,
		)
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, scoreFailureMessage(scoreErr))
		return
	}
	h.recordScore(ctx, outcome, experiment, item, binding, version, &scored.Score, scored.Explanation, scored.Details, "")
}

func (h *Handler) recordScore(ctx context.Context, outcome sessionOutcome, experiment db.EvaluationExperiment, item db.EvaluationExperimentItem, binding db.EvaluationExperimentBinding, version db.EvaluationEvaluatorVersion, score *float64, explanation string, details json.RawMessage, errorMessage string) {
	externalID, err := ids.New("evalscore_")
	if err != nil {
		h.logger.WarnContext(ctx, "create evaluation score id",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", experiment.ExternalID,
			"error", err,
		)
		return
	}
	status := db.EvaluationExperimentScoreStatusFailed
	if errorMessage == "" {
		status = db.EvaluationExperimentScoreStatusCompleted
	}
	versionUUID := version.UUID
	if versionUUID == "" {
		versionUUID = binding.VersionUUID
	}
	evaluatorUUID := version.EvaluatorUUID
	if evaluatorUUID == "" {
		evaluatorUUID = binding.EvaluatorUUID
	}
	_, err = h.store.CreateEvaluationExperimentScore(ctx, db.EvaluationExperimentScore{
		UUID:                uuid.NewString(),
		ExternalID:          externalID,
		OrganizationUUID:    outcome.OrganizationUUID,
		WorkspaceUUID:       outcome.WorkspaceUUID,
		ExperimentUUID:      item.ExperimentUUID,
		ItemUUID:            item.UUID,
		ItemExternalID:      item.ExternalID,
		EvaluatorUUID:       evaluatorUUID,
		EvaluatorExternalID: binding.EvaluatorExternalID,
		VersionUUID:         versionUUID,
		VersionExternalID:   binding.VersionExternalID,
		Position:            binding.Position,
		Status:              status,
		Score:               score,
		Explanation:         truncateRunes(explanation, maxScoreExplanationRunes),
		Details:             details,
		ErrorMessage:        truncateRunes(errorMessage, maxScoreErrorRunes),
		CreatedAt:           time.Now().UTC(),
	})
	if errors.Is(err, db.ErrDuplicate) {
		return
	}
	if err != nil {
		h.logger.WarnContext(ctx, "store evaluation score",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", experiment.ExternalID,
			"error", err,
		)
	}
}

func scoreFailureMessage(err error) string {
	switch {
	case errors.Is(err, errEvaluatorScoreParse):
		return evaluatorScoreParseMessage
	case errors.Is(err, errEvaluatorModelMissing):
		return evaluatorModelMissingMessage
	default:
		return evaluatorScoreFailedMessage
	}
}

func (h *Handler) loadScoreValues(ctx context.Context, outcome sessionOutcome, experiment db.EvaluationExperiment, item db.EvaluationExperimentItem) (scoreValues, error) {
	cases, err := h.store.ListEvaluationCasesByVersion(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, experiment.VersionUUID)
	if err != nil {
		return scoreValues{}, err
	}
	var matched db.EvaluationCase
	for _, candidate := range cases {
		if candidate.ExternalID == item.CaseExternalID {
			matched = candidate
			break
		}
	}
	events, err := h.sessionScoreEvents(ctx, outcome.WorkspaceUUID, outcome.SessionExternalID)
	if err != nil {
		return scoreValues{}, err
	}
	return scoreValuesFrom(matched, events), nil
}

func (h *Handler) sessionScoreEvents(ctx context.Context, workspaceUUID, sessionExternalID string) ([]db.SessionEvent, error) {
	var events []db.SessionEvent
	var cursor *db.SessionEventPageCursor
	for page := 0; page < maxScoreEventPages; page++ {
		batch, more, err := h.store.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
			WorkspaceUUID:     workspaceUUID,
			SessionExternalID: sessionExternalID,
			Limit:             scoreEventPageSize,
			Cursor:            cursor,
			Order:             "asc",
		})
		if err != nil {
			return nil, err
		}
		events = append(events, batch...)
		if !more || len(batch) == 0 {
			break
		}
		cursor = &db.SessionEventPageCursor{ExternalID: batch[len(batch)-1].ExternalID}
	}
	return events, nil
}

func scoreValuesFrom(item db.EvaluationCase, events []db.SessionEvent) scoreValues {
	values := scoreValues{Checkpoints: jsonText(item.Checkpoints)}
	var input, output, trajectory strings.Builder
	var toolResult string
	var sawToolResult bool
	var stopReason string
	for _, event := range events {
		text := messageText(event.Payload)
		switch event.EventType {
		case "user.message":
			appendScoreLine(&input, text)
			appendScoreLine(&trajectory, event.EventType+": "+text)
		case "agent.message":
			appendScoreLine(&output, text)
			appendScoreLine(&trajectory, event.EventType+": "+text)
		case "agent.tool_result":
			toolResult = text
			sawToolResult = true
			appendScoreLine(&trajectory, event.EventType+": "+text)
		case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
			appendScoreLine(&trajectory, event.EventType+": "+text)
		case "session.status_idle":
			if reason := stopReasonType(event.Payload); reason != "" {
				stopReason = reason
			}
		}
	}
	if input.Len() == 0 {
		values.Input = truncateRunes(strings.Join(item.Turns, "\n"), maxScoreVariableRunes)
	} else {
		values.Input = truncateRunes(input.String(), maxScoreVariableRunes)
	}
	values.Output = truncateRunes(output.String(), maxScoreVariableRunes)
	values.AgentTrajectory = truncateRunes(trajectory.String(), maxScoreVariableRunes)
	if sawToolResult {
		values.InvocationResult = truncateRunes(toolResult, maxScoreVariableRunes)
	} else {
		values.InvocationResult = truncateRunes(stopReason, maxScoreVariableRunes)
	}
	return values
}

func renderEvaluatorPrompt(template string, variables []string, values scoreValues) string {
	rendered := template
	for _, name := range variables {
		rendered = strings.ReplaceAll(rendered, "{{"+name+"}}", scoreVariable(name, values))
	}
	return rendered
}

func scoreVariable(name string, values scoreValues) string {
	switch name {
	case "input":
		return values.Input
	case "output":
		return values.Output
	case "checkpoints":
		return values.Checkpoints
	case "agent_trajectory":
		return values.AgentTrajectory
	case "invocation_result":
		return values.InvocationResult
	default:
		return ""
	}
}

func messageText(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var body struct {
		Content json.RawMessage `json:"content"`
		Text    string          `json:"text"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return ""
	}
	if text := contentText(body.Content); text != "" {
		return text
	}
	return body.Text
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch {
		case block.Text != "":
			parts = append(parts, block.Text)
		case block.Name != "":
			parts = append(parts, block.Name)
		}
	}
	return strings.Join(parts, "\n")
}

func appendScoreLine(builder *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	builder.WriteString(text)
}

func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}
