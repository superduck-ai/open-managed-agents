package evaluations

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/google/uuid"
)

type experimentEvaluatorResponse struct {
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	Position  int    `json:"position"`
}

type experimentItemResponse struct {
	ID        string `json:"id"`
	CaseID    string `json:"case_id"`
	Position  int    `json:"position"`
	SessionID string `json:"session_id,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

type experimentResponse struct {
	ID             string                        `json:"id"`
	Type           string                        `json:"type"`
	Name           string                        `json:"name"`
	Desc           string                        `json:"desc"`
	Status         string                        `json:"status"`
	DatasetID      string                        `json:"dataset_id"`
	VersionID      string                        `json:"version_id"`
	AgentID        string                        `json:"agent_id"`
	AgentVersion   int                           `json:"agent_version"`
	EnvironmentID  string                        `json:"environment_id"`
	TimeoutSeconds int                           `json:"timeout_seconds"`
	Env            json.RawMessage               `json:"env,omitempty"`
	Resources      json.RawMessage               `json:"resources,omitempty"`
	ItemCount      int                           `json:"item_count"`
	SessionCount   int                           `json:"session_count"`
	EvaluatorCount int                           `json:"evaluator_count"`
	Evaluators     []experimentEvaluatorResponse `json:"evaluators,omitempty"`
	Items          []experimentItemResponse      `json:"items,omitempty"`
	CreatedAt      string                        `json:"created_at"`
	UpdatedAt      string                        `json:"updated_at"`
}

type experimentPageResponse struct {
	Data     []experimentResponse `json:"data"`
	NextPage *string              `json:"next_page"`
}

func experimentResponseFrom(experiment db.EvaluationExperiment, includeChildren bool) experimentResponse {
	response := experimentResponse{
		ID:             experiment.ExternalID,
		Type:           "evaluation_experiment",
		Name:           experiment.Name,
		Desc:           experiment.Description,
		Status:         experiment.Status,
		DatasetID:      experiment.DatasetExternalID,
		VersionID:      experiment.VersionExternalID,
		AgentID:        experiment.AgentExternalID,
		AgentVersion:   experiment.AgentVersion,
		EnvironmentID:  experiment.EnvironmentExternalID,
		TimeoutSeconds: experiment.TimeoutSeconds,
		Env:            experiment.Env,
		Resources:      experiment.Resources,
		ItemCount:      int(experiment.ItemCount),
		SessionCount:   int(experiment.SessionCount),
		EvaluatorCount: int(experiment.EvaluatorCount),
		CreatedAt:      experiment.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      experiment.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if !includeChildren {
		response.Env = nil
		response.Resources = nil
		return response
	}
	response.Evaluators = experimentEvaluatorsFrom(experiment.Bindings)
	response.Items = experimentItemsFrom(experiment.Items)
	if response.EvaluatorCount == 0 {
		response.EvaluatorCount = len(response.Evaluators)
	}
	if response.ItemCount == 0 {
		response.ItemCount = len(response.Items)
	}
	return response
}

func experimentPage(experiments []db.EvaluationExperiment, hasMore bool, includeEvaluators bool) experimentPageResponse {
	data := make([]experimentResponse, 0, len(experiments))
	for _, experiment := range experiments {
		data = append(data, experimentResponseFrom(experiment, includeEvaluators))
	}
	var nextPage *string
	if hasMore && len(experiments) > 0 {
		last := experiments[len(experiments)-1]
		value := encodeCursor(datasetCursorWire{CreatedAt: last.CreatedAt, UUID: last.UUID})
		nextPage = &value
	}
	return experimentPageResponse{Data: data, NextPage: nextPage}
}

func experimentEvaluatorsFrom(bindings []db.EvaluationExperimentBinding) []experimentEvaluatorResponse {
	evaluators := make([]experimentEvaluatorResponse, 0, len(bindings))
	for _, binding := range bindings {
		evaluators = append(evaluators, experimentEvaluatorResponse{
			ID:        binding.EvaluatorExternalID,
			VersionID: binding.VersionExternalID,
			Position:  binding.Position,
		})
	}
	return evaluators
}

func experimentItemsFrom(items []db.EvaluationExperimentItem) []experimentItemResponse {
	responses := make([]experimentItemResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, experimentItemResponse{
			ID:        item.ExternalID,
			CaseID:    item.CaseExternalID,
			Position:  item.Position,
			SessionID: item.SessionExternalID,
			Status:    item.Status,
			Error:     item.ErrorMessage,
		})
	}
	return responses
}

func decodeExperimentCursor(raw string) (*db.EvaluationExperimentPageCursor, error) {
	cursor, err := decodeTimeUUIDCursor(raw)
	if err != nil || cursor == nil {
		return nil, err
	}
	parsed, parseErr := uuid.Parse(strings.TrimSpace(cursor.UUID))
	if parseErr != nil {
		return nil, invalidPage(parseErr)
	}
	return &db.EvaluationExperimentPageCursor{CreatedAt: cursor.CreatedAt, UUID: parsed.String()}, nil
}
