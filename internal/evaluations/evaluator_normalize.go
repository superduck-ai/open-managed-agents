package evaluations

import (
	"strings"
	"unicode/utf8"
)

const (
	evaluatorTypeLLM       = "llm"
	evaluatorTypeAgent     = "agent"
	maxEvaluatorModelLen   = 255
	maxEvaluatorAgentIDLen = 255
	maxEvaluatorPromptLen  = 16000
	evaluatorResultPath    = "/data/session/result.json"
	evaluatorScorePath     = "/output/score.json"
)

var evaluatorVariableOrder = []string{"input", "output", "checkpoints", "agent_trajectory", "invocation_result"}

var evaluatorScoreFields = []string{"score", "explanation", "details"}

type createEvaluatorRequest struct {
	Name            string   `json:"name"`
	Desc            string   `json:"desc"`
	EvaluatorType   string   `json:"evaluator_type"`
	Model           string   `json:"model"`
	AgentID         string   `json:"agent_id"`
	PromptTemplate  string   `json:"prompt_template"`
	Variables       []string `json:"variables"`
	SourceVersionID string   `json:"source_version_id"`
}

type normalizedEvaluator struct {
	Name           string
	Description    string
	EvaluatorType  string
	Model          string
	AgentID        string
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
	variables, err := normalizeEvaluatorVariables(body.Variables, kind == evaluatorTypeLLM)
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

func normalizeLLMEvaluator(item normalizedEvaluator, body createEvaluatorRequest) (normalizedEvaluator, error) {
	if strings.TrimSpace(body.AgentID) != "" {
		return normalizedEvaluator{}, agentIDForbidden()
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
	if strings.TrimSpace(body.PromptTemplate) != "" {
		return normalizedEvaluator{}, promptForbidden()
	}
	agentID := strings.TrimSpace(body.AgentID)
	if agentID == "" {
		return normalizedEvaluator{}, agentIDRequired()
	}
	if utf8.RuneCountInString(agentID) > maxEvaluatorAgentIDLen {
		return normalizedEvaluator{}, agentIDTooLong()
	}
	item.AgentID = agentID
	return item, nil
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
