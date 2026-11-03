package evaluations

import (
	"bytes"
	"encoding/json"
	"strings"
)

const (
	maxExperimentEvaluators     = 20
	maxExperimentTimeoutSeconds = 86400
	maxExperimentItemErrorRunes = 1024
)

type createExperimentRequest struct {
	Name             string          `json:"name"`
	Desc             string          `json:"desc"`
	DatasetVersionID string          `json:"dataset_version_id"`
	AgentID          string          `json:"agent_id"`
	AgentVersion     *int            `json:"agent_version"`
	EnvironmentID    string          `json:"environment_id"`
	TimeoutSeconds   int             `json:"timeout_seconds"`
	Env              json.RawMessage `json:"env"`
	Resources        json.RawMessage `json:"resources"`
	EvaluatorIDs     []string        `json:"evaluator_ids"`
}

type compareExperimentsRequest struct {
	DatasetVersionID string   `json:"dataset_version_id"`
	EvaluatorIDs     []string `json:"evaluator_ids"`
}

type experimentDraft struct {
	Name             string
	Description      string
	DatasetVersionID string
	AgentID          string
	AgentVersion     int
	EnvironmentID    string
	TimeoutSeconds   int
	Env              json.RawMessage
	Resources        json.RawMessage
	EvaluatorIDs     []string
}

func normalizeExperiment(body createExperimentRequest) (experimentDraft, error) {
	name, description, err := normalizeNameAndDescription(body.Name, body.Desc)
	if err != nil {
		return experimentDraft{}, err
	}
	versionID, err := requiredExternalID(body.DatasetVersionID, datasetVersionRequired)
	if err != nil {
		return experimentDraft{}, err
	}
	agentID, err := requiredExternalID(body.AgentID, agentIDRequired)
	if err != nil {
		return experimentDraft{}, err
	}
	agentVersion, err := normalizeOptionalAgentVersion(body.AgentVersion)
	if err != nil {
		return experimentDraft{}, err
	}
	environmentID, err := requiredExternalID(body.EnvironmentID, environmentIDRequired)
	if err != nil {
		return experimentDraft{}, err
	}
	if body.TimeoutSeconds < 1 || body.TimeoutSeconds > maxExperimentTimeoutSeconds {
		return experimentDraft{}, timeoutInvalid()
	}
	evaluatorIDs, err := normalizeEvaluatorIDs(body.EvaluatorIDs)
	if err != nil {
		return experimentDraft{}, err
	}
	env, err := normalizeExperimentEnv(body.Env)
	if err != nil {
		return experimentDraft{}, err
	}
	resources, err := normalizeExperimentResources(body.Resources)
	if err != nil {
		return experimentDraft{}, err
	}
	return experimentDraft{
		Name:             name,
		Description:      description,
		DatasetVersionID: versionID,
		AgentID:          agentID,
		AgentVersion:     agentVersion,
		EnvironmentID:    environmentID,
		TimeoutSeconds:   body.TimeoutSeconds,
		Env:              env,
		Resources:        resources,
		EvaluatorIDs:     evaluatorIDs,
	}, nil
}

func normalizeCompare(body compareExperimentsRequest) (string, []string, error) {
	versionID, err := requiredExternalID(body.DatasetVersionID, datasetVersionRequired)
	if err != nil {
		return "", nil, err
	}
	evaluatorIDs, err := normalizeEvaluatorIDs(body.EvaluatorIDs)
	if err != nil {
		return "", nil, err
	}
	return versionID, evaluatorIDs, nil
}

func requiredExternalID(value string, missing func() error) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", missing()
	}
	return trimmed, nil
}

func normalizeOptionalAgentVersion(version *int) (int, error) {
	if version == nil {
		return 0, nil
	}
	if *version < 1 {
		return 0, agentVersionInvalid()
	}
	return *version, nil
}

func normalizeEvaluatorIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, evaluatorIDsRequired()
	}
	if len(values) > maxExperimentEvaluators {
		return nil, evaluatorIDsTooMany()
	}
	seen := make(map[string]struct{}, len(values))
	ids := make([]string, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			return nil, evaluatorIDsRequired()
		}
		if _, ok := seen[id]; ok {
			return nil, evaluatorIDsDuplicate()
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func normalizeExperimentEnv(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, envInvalid()
	}
	if len(values) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, envInvalid()
	}
	return encoded, nil
}

func normalizeExperimentResources(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, resourcesInvalid()
	}
	if len(items) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil, resourcesInvalid()
	}
	return encoded, nil
}
