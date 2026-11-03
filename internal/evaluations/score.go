package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
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
	maxTrajectoryDetailRunes        = 500
	maxScoreEventPages              = 5
	scoreEventPageSize              = 100
)

type scoreValues struct {
	Input            string
	Output           string
	Checkpoints      string
	CheckpointsJSON  json.RawMessage
	InputFiles       json.RawMessage
	ReferenceFiles   json.RawMessage
	AgentTrajectory  string
	InvocationResult string
}

func (h *Handler) scheduleExperimentScores(ctx context.Context, outcome sessionOutcome, experiment db.EvaluationExperiment) {
	for _, item := range experiment.Items {
		if item.Status != db.EvaluationExperimentStatusCompleted || item.SessionExternalID == "" {
			continue
		}
		h.scheduleCompletedItemScore(ctx, sessionOutcome{
			OrganizationUUID:  outcome.OrganizationUUID,
			WorkspaceUUID:     outcome.WorkspaceUUID,
			SessionExternalID: item.SessionExternalID,
			Status:            db.EvaluationExperimentStatusCompleted,
		}, db.EvaluationExperimentSessionMatch{Item: item, ExperimentExternalID: experiment.ExternalID})
	}
}

func (h *Handler) scheduleCompletedItemScore(ctx context.Context, outcome sessionOutcome, match db.EvaluationExperimentSessionMatch) {
	if h.scoreJobs == nil {
		h.scoreCompletedItem(ctx, outcome, match)
		return
	}
	enqueueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), scoreEnqueueTimeout)
	err := h.scoreJobs.EnqueueItem(enqueueCtx, outcome.OrganizationUUID, outcome.WorkspaceUUID, match.ExperimentExternalID, match.Item.ExternalID, outcome.SessionExternalID)
	cancel()
	if err != nil {
		h.logger.WarnContext(ctx, "enqueue evaluation score",
			"organization_id", outcome.OrganizationUUID,
			"workspace_id", outcome.WorkspaceUUID,
			"experiment_id", match.ExperimentExternalID,
			"item_id", match.Item.ExternalID,
			"error", err,
		)
	}
}

func (h *Handler) scoreQueuedItem(ctx context.Context, args scoreItemArgs) error {
	experiment, err := h.store.GetEvaluationExperiment(ctx, args.OrganizationUUID, args.WorkspaceUUID, args.ExperimentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if !itemsSettled(experiment.Items) {
		return nil
	}
	item, ok := experimentItemByID(experiment, args.ItemID)
	if !ok || item.Status != db.EvaluationExperimentStatusCompleted {
		return nil
	}
	sessionID := item.SessionExternalID
	if sessionID == "" {
		sessionID = args.SessionID
	}
	h.scoreCompletedItem(ctx, sessionOutcome{
		OrganizationUUID:  args.OrganizationUUID,
		WorkspaceUUID:     args.WorkspaceUUID,
		SessionExternalID: sessionID,
		Status:            db.EvaluationExperimentStatusCompleted,
	}, db.EvaluationExperimentSessionMatch{Item: item, ExperimentExternalID: experiment.ExternalID})
	return nil
}

func (h *Handler) rescueUnscoredItems(ctx context.Context) error {
	if h.scoreJobs == nil {
		return nil
	}
	if err := h.store.ReconcileSettledEvaluationExperiments(ctx, time.Now().UTC()); err != nil {
		return err
	}
	items, err := h.store.ListEvaluationItemsMissingScores(ctx, scoreRescueLimit)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.SessionExternalID == "" {
			continue
		}
		if err := h.scoreJobs.EnqueueItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.ExperimentExternalID, item.ItemExternalID, item.SessionExternalID); err != nil {
			return err
		}
	}
	return nil
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
	if itemBindingScored(experiment, item.ExternalID, binding.VersionExternalID) {
		return
	}
	version, err := h.evaluatorVersionForScore(ctx, outcome, binding.VersionExternalID)
	if err != nil {
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, evaluatorVersionNotFoundMessage)
		return
	}
	if version.EvaluatorType == evaluatorTypeAgent && strings.TrimSpace(version.AgentID) != "" {
		h.scoreAgent(ctx, outcome, experiment, item, binding, version, values)
		return
	}
	if h.model == nil {
		h.recordScore(ctx, outcome, experiment, item, binding, version, nil, "", nil, evaluatorModelMissingMessage)
		return
	}
	scoreCtx, cancel := context.WithTimeout(ctx, evaluatorScoreTimeout)
	scored, scoreErr := h.model.Score(scoreCtx, EvaluatorScoreRequest{
		OrganizationUUID: outcome.OrganizationUUID,
		WorkspaceUUID:    outcome.WorkspaceUUID,
		Model:            version.Model,
		Prompt:           renderEvaluatorPrompt(version.PromptTemplate, version.Variables, values),
	})
	cancel()
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

func (h *Handler) evaluatorVersionForScore(ctx context.Context, outcome sessionOutcome, versionID string) (db.EvaluationEvaluatorVersion, error) {
	version, err := h.store.GetEvaluationEvaluatorVersion(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, versionID)
	if err == nil {
		return version, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return db.EvaluationEvaluatorVersion{}, err
	}
	preset, presetErr := presetEvaluatorVersion(versionID)
	if presetErr != nil || preset.Archived {
		return db.EvaluationEvaluatorVersion{}, err
	}
	return preset.asRecord().Version, nil
}

func itemBindingScored(experiment db.EvaluationExperiment, itemID, versionID string) bool {
	for _, score := range experiment.Scores {
		if score.ItemExternalID == itemID && score.VersionExternalID == versionID {
			return true
		}
	}
	return false
}

func (h *Handler) recordScore(ctx context.Context, outcome sessionOutcome, experiment db.EvaluationExperiment, item db.EvaluationExperimentItem, binding db.EvaluationExperimentBinding, version db.EvaluationEvaluatorVersion, score *float64, explanation string, details json.RawMessage, errorMessage string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), scoreWriteTimeout)
	defer cancel()
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
	case errors.Is(err, ErrAgentScoreFileMissing):
		return agentScoreFileMissingMessage
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
	values := scoreValues{
		Checkpoints:     jsonText(item.Checkpoints),
		CheckpointsJSON: jsonValueOrNull(item.Checkpoints),
		InputFiles:      jsonArrayOrEmpty(item.InputFiles),
		ReferenceFiles:  jsonArrayOrEmpty(item.ReferenceFiles),
	}
	var input, output, trajectory strings.Builder
	var toolResult string
	var sawToolResult bool
	var stopReason string
	for _, event := range events {
		text := messageText(event.Payload)
		switch event.EventType {
		case "user.message":
			appendScoreLine(&input, text)
		case "agent.message":
			appendScoreLine(&output, text)
		case "agent.tool_result":
			toolResult = text
			sawToolResult = true
		case "session.status_idle":
			if reason := stopReasonType(event.Payload); reason != "" {
				stopReason = reason
			}
		}
		appendScoreLine(&trajectory, trajectoryLine(event))
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

func trajectoryLine(event db.SessionEvent) string {
	if event.EventType == "" {
		return ""
	}
	var line strings.Builder
	if stamp := scoreEventTime(event); stamp != "" {
		line.WriteString(stamp)
		line.WriteByte(' ')
	}
	line.WriteString(event.EventType)
	if detail := trajectoryDetail(event); detail != "" {
		line.WriteString(": ")
		line.WriteString(detail)
	}
	return line.String()
}

func scoreEventTime(event db.SessionEvent) string {
	stamp := event.CreatedAt
	if stamp.IsZero() {
		stamp = event.ProcessedAt
	}
	if stamp.IsZero() {
		return ""
	}
	return stamp.UTC().Format(time.RFC3339)
}

func trajectoryDetail(event db.SessionEvent) string {
	switch event.EventType {
	case "span.model_request_start", "span.model_request_end":
		return modelRequestDetail(event.Payload)
	case "session.usage":
		return usageDetail(event.Payload)
	case "session.status_idle", "session.thread_status_idle", "session.status_terminated":
		if reason := stopReasonType(event.Payload); reason != "" {
			return "stop_reason=" + reason
		}
		return ""
	default:
		return truncateRunes(messageText(event.Payload), maxTrajectoryDetailRunes)
	}
}

func modelRequestDetail(payload json.RawMessage) string {
	var body struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens  *int64 `json:"input_tokens"`
			OutputTokens *int64 `json:"output_tokens"`
		} `json:"model_usage"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &body) != nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if model := strings.TrimSpace(body.Model); model != "" {
		parts = append(parts, "model="+model)
	}
	if body.Usage.InputTokens != nil {
		parts = append(parts, "input_tokens="+strconv.FormatInt(*body.Usage.InputTokens, 10))
	}
	if body.Usage.OutputTokens != nil {
		parts = append(parts, "output_tokens="+strconv.FormatInt(*body.Usage.OutputTokens, 10))
	}
	if reason := strings.TrimSpace(body.Error.Type); reason != "" {
		parts = append(parts, "error="+reason)
	}
	return strings.Join(parts, " ")
}

func usageDetail(payload json.RawMessage) string {
	var body struct {
		Usage struct {
			InputTokens   any `json:"input_tokens"`
			OutputTokens  any `json:"output_tokens"`
			ActiveSeconds any `json:"active_seconds"`
			ListCost      struct {
				Amount any `json:"amount"`
			} `json:"list_cost"`
		} `json:"usage"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &body) != nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if value := metricString(body.Usage.InputTokens); value != "" {
		parts = append(parts, "input_tokens="+value)
	}
	if value := metricString(body.Usage.OutputTokens); value != "" {
		parts = append(parts, "output_tokens="+value)
	}
	if value := metricString(body.Usage.ListCost.Amount); value != "" {
		parts = append(parts, "list_cost="+value)
	}
	if value := metricString(body.Usage.ActiveSeconds); value != "" {
		parts = append(parts, "active_seconds="+value)
	}
	return strings.Join(parts, " ")
}

func metricString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
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

func jsonValueOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" || !json.Valid(raw) {
		return json.RawMessage(`null`)
	}
	return raw
}

func jsonArrayOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" || !json.Valid(raw) {
		return json.RawMessage(`[]`)
	}
	return raw
}
