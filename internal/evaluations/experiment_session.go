package evaluations

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

const (
	sessionTerminatedMessage       = "Session terminated"
	sessionRetriesExhaustedMessage = "Session retries exhausted"
)

type sessionOutcome struct {
	OrganizationUUID  string
	WorkspaceUUID     string
	SessionExternalID string
	Status            string
	ErrorMessage      string
}

func (h *Handler) ObserveSessionEvents(ctx context.Context, events []db.SessionEvent) {
	outcomes := sessionOutcomes(events)
	if len(outcomes) == 0 {
		return
	}
	updated := map[string]sessionOutcome{}
	for _, outcome := range outcomes {
		matches, err := h.store.ListRunningEvaluationExperimentItemsBySession(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, outcome.SessionExternalID)
		if err != nil {
			h.logger.WarnContext(ctx, "list evaluation items for session",
				"organization_id", outcome.OrganizationUUID,
				"workspace_id", outcome.WorkspaceUUID,
				"session_id", outcome.SessionExternalID,
				"error", err,
			)
			continue
		}
		for _, match := range matches {
			finished, finishErr := h.store.FinishRunningEvaluationExperimentItem(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, match.Item.ExternalID, outcome.Status, outcome.ErrorMessage, time.Now().UTC())
			if finishErr != nil {
				h.logger.WarnContext(ctx, "finish evaluation experiment item",
					"organization_id", outcome.OrganizationUUID,
					"workspace_id", outcome.WorkspaceUUID,
					"session_id", outcome.SessionExternalID,
					"experiment_id", match.ExperimentExternalID,
					"error", finishErr,
				)
				continue
			}
			if !finished {
				continue
			}
			if outcome.Status == db.EvaluationExperimentStatusCompleted {
				h.scoreCompletedItem(ctx, outcome, match)
			}
			updated[match.ExperimentExternalID] = outcome
		}
	}
	for experimentID, outcome := range updated {
		if err := h.rollUpExperiment(ctx, outcome, experimentID); err != nil {
			h.logger.WarnContext(ctx, "roll up evaluation experiment",
				"organization_id", outcome.OrganizationUUID,
				"workspace_id", outcome.WorkspaceUUID,
				"experiment_id", experimentID,
				"error", err,
			)
		}
	}
}

func (h *Handler) rollUpExperiment(ctx context.Context, outcome sessionOutcome, experimentID string) error {
	experiment, err := h.store.GetEvaluationExperiment(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, experimentID)
	if err != nil {
		return err
	}
	status := experimentStatus(experiment.Items)
	if status == experiment.Status {
		return nil
	}
	return h.store.UpdateEvaluationExperimentStatus(ctx, outcome.OrganizationUUID, outcome.WorkspaceUUID, experimentID, status, time.Now().UTC())
}

func sessionOutcomes(events []db.SessionEvent) []sessionOutcome {
	index := map[string]int{}
	outcomes := make([]sessionOutcome, 0)
	for _, event := range events {
		outcome, ok := sessionOutcomeFromEvent(event)
		if !ok {
			continue
		}
		key := outcome.OrganizationUUID + "\x00" + outcome.WorkspaceUUID + "\x00" + outcome.SessionExternalID
		if pos, exists := index[key]; exists {
			if outcomes[pos].Status == db.EvaluationExperimentStatusFailed && outcome.Status == db.EvaluationExperimentStatusCompleted {
				continue
			}
			outcomes[pos] = outcome
			continue
		}
		index[key] = len(outcomes)
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

func sessionOutcomeFromEvent(event db.SessionEvent) (sessionOutcome, bool) {
	if event.OrganizationUUID == "" || event.WorkspaceUUID == "" || event.SessionExternalID == "" {
		return sessionOutcome{}, false
	}
	status, ok := maevents.SessionStatus(event.EventType)
	if !ok {
		return sessionOutcome{}, false
	}
	outcome := sessionOutcome{
		OrganizationUUID:  event.OrganizationUUID,
		WorkspaceUUID:     event.WorkspaceUUID,
		SessionExternalID: event.SessionExternalID,
	}
	switch status {
	case "terminated":
		outcome.Status = db.EvaluationExperimentStatusFailed
		outcome.ErrorMessage = sessionTerminatedMessage
		return outcome, true
	case "idle":
		switch stopReasonType(event.Payload) {
		case "retries_exhausted":
			outcome.Status = db.EvaluationExperimentStatusFailed
			outcome.ErrorMessage = sessionRetriesExhaustedMessage
			return outcome, true
		case "end_turn":
			outcome.Status = db.EvaluationExperimentStatusCompleted
			return outcome, true
		default:
			return sessionOutcome{}, false
		}
	default:
		return sessionOutcome{}, false
	}
}

func stopReasonType(payload json.RawMessage) string {
	var body struct {
		StopReason struct {
			Type string `json:"type"`
		} `json:"stop_reason"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &body) != nil {
		return ""
	}
	return body.StopReason.Type
}
