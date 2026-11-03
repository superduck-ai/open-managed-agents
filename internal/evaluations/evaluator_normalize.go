package evaluations

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	evaluatorTypeLLM      = "llm"
	evaluatorTypeAgent    = "agent"
	maxEvaluatorModelLen  = 255
	maxEvaluatorPromptLen = 16000
	maxEvaluatorMounts    = 20
	evaluatorResultPath   = "/data/session/result.json"
	evaluatorScorePath    = "/output/score.json"
)

var evaluatorVariableOrder = []string{"input", "output", "checkpoints", "agent_trajectory", "invocation_result"}

var evaluatorScoreFields = []string{"score", "explanation", "details"}

type evaluatorVersionRequest struct {
	Model          string          `json:"model"`
	AgentID        string          `json:"agent_id"`
	EnvironmentID  string          `json:"environment_id"`
	Resources      json.RawMessage `json:"resources"`
	PromptTemplate string          `json:"prompt_template"`
	Variables      []string        `json:"variables"`
}

type createEvaluatorRequest struct {
	Name            string          `json:"name"`
	Desc            string          `json:"desc"`
	EvaluatorType   string          `json:"evaluator_type"`
	Model           string          `json:"model"`
	AgentID         string          `json:"agent_id"`
	EnvironmentID   string          `json:"environment_id"`
	Resources       json.RawMessage `json:"resources"`
	PromptTemplate  string          `json:"prompt_template"`
	Variables       []string        `json:"variables"`
	SourceVersionID string          `json:"source_version_id"`
}

type evaluatorMountRequest struct {
	FileID string `json:"file_id"`
	Path   string `json:"path"`
}

type storedEvaluatorMount struct {
	Type      string `json:"type"`
	FileID    string `json:"file_id"`
	MountPath string `json:"mount_path"`
}

type normalizedEvaluator struct {
	Name           string
	Description    string
	EvaluatorType  string
	Model          string
	AgentID        string
	EnvironmentID  string
	Resources      json.RawMessage
	PromptTemplate string
	Variables      []string
}

func normalizeEvaluator(body createEvaluatorRequest) (normalizedEvaluator, error) {
	name, description, err := normalizeNameAndDescription(body.Name, body.Desc)
	if err != nil {
		return normalizedEvaluator{}, err
	}
	kind, err := normalizeEvaluatorType(body.EvaluatorType)
	if err != nil {
		return normalizedEvaluator{}, err
	}
	variables, err := normalizeEvaluatorVariables(body.Variables, true)
	if err != nil {
		return normalizedEvaluator{}, err
	}
	item := normalizedEvaluator{Name: name, Description: description, EvaluatorType: kind, Variables: variables}
	if kind == evaluatorTypeLLM {
		return normalizeLLMEvaluator(item, body)
	}
	return normalizeAgentEvaluator(item, body)
}

func normalizeEvaluatorType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case evaluatorTypeLLM, evaluatorTypeAgent:
		return strings.TrimSpace(value), nil
	default:
		return "", evaluatorTypeInvalid()
	}
}

func normalizeEvaluatorVariables(values []string, requireOne bool) ([]string, error) {
	if values == nil {
		values = []string{}
	}
	selected := make(map[string]struct{}, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if !isEvaluatorVariable(name) {
			return nil, variablesInvalid()
		}
		if _, exists := selected[name]; exists {
			return nil, variablesDuplicate()
		}
		selected[name] = struct{}{}
	}
	if requireOne && len(selected) == 0 {
		return nil, variablesRequired()
	}
	ordered := make([]string, 0, len(selected))
	for _, name := range evaluatorVariableOrder {
		if _, ok := selected[name]; ok {
			ordered = append(ordered, name)
		}
	}
	return ordered, nil
}

func nextEvaluatorVersion(current db.EvaluationEvaluatorVersion, body evaluatorVersionRequest) (db.EvaluationEvaluatorVersion, error) {
	switch current.EvaluatorType {
	case evaluatorTypeLLM:
		return nextLLMEvaluatorVersion(body)
	case evaluatorTypeAgent:
		return nextAgentEvaluatorVersion(current, body)
	default:
		return db.EvaluationEvaluatorVersion{}, evaluatorTypeInvalid()
	}
}

func nextLLMEvaluatorVersion(body evaluatorVersionRequest) (db.EvaluationEvaluatorVersion, error) {
	variables, err := normalizeEvaluatorVariables(body.Variables, true)
	if err != nil {
		return db.EvaluationEvaluatorVersion{}, err
	}
	item, err := normalizeLLMEvaluator(normalizedEvaluator{Variables: variables}, createEvaluatorRequest{
		Model:          body.Model,
		AgentID:        body.AgentID,
		EnvironmentID:  body.EnvironmentID,
		Resources:      body.Resources,
		PromptTemplate: body.PromptTemplate,
		Variables:      variables,
	})
	if err != nil {
		return db.EvaluationEvaluatorVersion{}, err
	}
	return db.EvaluationEvaluatorVersion{
		EvaluatorType:  evaluatorTypeLLM,
		Model:          item.Model,
		PromptTemplate: item.PromptTemplate,
		Variables:      item.Variables,
	}, nil
}

func nextAgentEvaluatorVersion(_ db.EvaluationEvaluatorVersion, body evaluatorVersionRequest) (db.EvaluationEvaluatorVersion, error) {
	item, err := normalizeAgentEvaluator(normalizedEvaluator{}, createEvaluatorRequest{
		Model:          body.Model,
		AgentID:        body.AgentID,
		EnvironmentID:  body.EnvironmentID,
		Resources:      body.Resources,
		PromptTemplate: body.PromptTemplate,
		Variables:      body.Variables,
	})
	if err != nil {
		return db.EvaluationEvaluatorVersion{}, err
	}
	return db.EvaluationEvaluatorVersion{
		EvaluatorType:  evaluatorTypeAgent,
		AgentID:        item.AgentID,
		EnvironmentID:  item.EnvironmentID,
		Resources:      item.Resources,
		PromptTemplate: item.PromptTemplate,
		Variables:      item.Variables,
	}, nil
}

func normalizeLLMEvaluator(item normalizedEvaluator, body createEvaluatorRequest) (normalizedEvaluator, error) {
	if strings.TrimSpace(body.AgentID) != "" {
		return normalizedEvaluator{}, agentIDForbidden()
	}
	if strings.TrimSpace(body.EnvironmentID) != "" || evaluatorResourcesPresent(body.Resources) {
		return normalizedEvaluator{}, llmSessionConfigForbidden()
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		return normalizedEvaluator{}, modelRequired()
	}
	if utf8.RuneCountInString(model) > maxEvaluatorModelLen {
		return normalizedEvaluator{}, modelTooLong()
	}
	prompt := strings.TrimSpace(body.PromptTemplate)
	if prompt == "" {
		return normalizedEvaluator{}, promptRequired()
	}
	if utf8.RuneCountInString(prompt) > maxEvaluatorPromptLen {
		return normalizedEvaluator{}, promptTooLong()
	}
	if err := validatePromptPlaceholders(prompt, item.Variables); err != nil {
		return normalizedEvaluator{}, err
	}
	item.Model = model
	item.PromptTemplate = prompt
	return item, nil
}

func normalizeAgentEvaluator(item normalizedEvaluator, body createEvaluatorRequest) (normalizedEvaluator, error) {
	if strings.TrimSpace(body.Model) != "" {
		return normalizedEvaluator{}, modelForbidden()
	}
	agentID := strings.TrimSpace(body.AgentID)
	if agentID == "" {
		return normalizedEvaluator{}, agentIDRequired()
	}
	if utf8.RuneCountInString(agentID) > maxEvaluatorModelLen {
		return normalizedEvaluator{}, agentIDTooLong()
	}
	environmentID := strings.TrimSpace(body.EnvironmentID)
	if utf8.RuneCountInString(environmentID) > maxEvaluatorModelLen {
		return normalizedEvaluator{}, environmentIDTooLong()
	}
	resources, err := normalizeEvaluatorMounts(body.Resources)
	if err != nil {
		return normalizedEvaluator{}, err
	}
	prompt := strings.TrimSpace(body.PromptTemplate)
	if prompt == "" {
		return normalizedEvaluator{}, promptRequired()
	}
	if utf8.RuneCountInString(prompt) > maxEvaluatorPromptLen {
		return normalizedEvaluator{}, promptTooLong()
	}
	variables, err := normalizeEvaluatorVariables(body.Variables, true)
	if err != nil {
		return normalizedEvaluator{}, err
	}
	if err := validatePromptPlaceholders(prompt, variables); err != nil {
		return normalizedEvaluator{}, err
	}
	item.AgentID = agentID
	item.EnvironmentID = environmentID
	item.Resources = resources
	item.Variables = variables
	item.PromptTemplate = prompt
	return item, nil
}

func evaluatorResourcesPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && string(trimmed) != "null" && string(trimmed) != "[]"
}

func normalizeEvaluatorMounts(raw json.RawMessage) (json.RawMessage, error) {
	if !evaluatorResourcesPresent(raw) {
		return nil, nil
	}
	var mounts []evaluatorMountRequest
	if err := json.Unmarshal(raw, &mounts); err != nil {
		return nil, evaluatorMountInvalid()
	}
	if len(mounts) == 0 || len(mounts) > maxEvaluatorMounts {
		return nil, evaluatorMountInvalid()
	}
	stored := make([]storedEvaluatorMount, 0, len(mounts))
	seen := make(map[string]struct{}, len(mounts))
	for _, mount := range mounts {
		fileID := strings.TrimSpace(mount.FileID)
		path := strings.TrimSpace(mount.Path)
		if fileID == "" || utf8.RuneCountInString(fileID) > maxEvaluatorModelLen || !validEvaluatorMountPath(path) {
			return nil, evaluatorMountInvalid()
		}
		if _, ok := seen[path]; ok {
			return nil, evaluatorMountInvalid()
		}
		seen[path] = struct{}{}
		stored = append(stored, storedEvaluatorMount{Type: "file", FileID: fileID, MountPath: path})
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		return nil, evaluatorMountInvalid()
	}
	return payload, nil
}

func validEvaluatorMountPath(path string) bool {
	if path == "" || utf8.RuneCountInString(path) > maxEvaluatorModelLen || !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	if strings.Contains(path, "..") || strings.ContainsAny(path, "\n\r") {
		return false
	}
	return true
}

func validatePromptPlaceholders(template string, selected []string) error {
	allowed := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		allowed[name] = struct{}{}
	}
	rest := template
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			return nil
		}
		end := strings.Index(rest[start+2:], "}}")
		if end < 0 {
			return promptVariableInvalid()
		}
		name := strings.TrimSpace(rest[start+2 : start+2+end])
		if _, ok := allowed[name]; !ok {
			return promptVariableInvalid()
		}
		rest = rest[start+2+end+2:]
	}
}

func isEvaluatorVariable(name string) bool {
	for _, candidate := range evaluatorVariableOrder {
		if candidate == name {
			return true
		}
	}
	return false
}
