package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type cmaMultiagent struct {
	Type      string        `json:"type"`
	Subagents cmaDelegation `json:"subagents"`
	Workflows cmaDelegation `json:"workflows"`
	Advisor   cmaAdvisor    `json:"advisor"`
}

type cmaDelegation struct {
	Type             string           `json:"type"`
	InlineAgents     *cmaSwitch       `json:"inline_agents,omitempty"`
	PredefinedAgents []agentReference `json:"predefined_agents,omitzero"`
}

type cmaSwitch struct {
	Type string `json:"type"`
}

type cmaAdvisor struct {
	Type  string `json:"type"`
	Model string `json:"model,omitempty"`
}

func (h *Handler) normalizeCMAMultiagent(r *http.Request, principal auth.Principal, selfID string, selfVersion int, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Subagents json.RawMessage `json:"subagents"`
		Workflows json.RawMessage `json:"workflows"`
		Advisor   json.RawMessage `json:"advisor"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, errors.New("multiagent must be an object")
	}
	subagents, err := h.normalizeCMADelegation(r, principal, selfID, selfVersion, input.Subagents)
	if err != nil {
		return nil, fmt.Errorf("multiagent.subagents: %w", err)
	}
	workflows, err := h.normalizeCMADelegation(r, principal, selfID, selfVersion, input.Workflows)
	if err != nil {
		return nil, fmt.Errorf("multiagent.workflows: %w", err)
	}
	advisor, err := normalizeCMAAdvisor(input.Advisor)
	if err != nil {
		return nil, err
	}
	return jsonx.Encode(cmaMultiagent{Type: "multiagent_20261001", Subagents: subagents, Workflows: workflows, Advisor: advisor})
}

func (h *Handler) normalizeCMADelegation(r *http.Request, principal auth.Principal, selfID string, selfVersion int, raw json.RawMessage) (cmaDelegation, error) {
	result := cmaDelegation{Type: "enabled", InlineAgents: &cmaSwitch{Type: "enabled"}, PredefinedAgents: []agentReference{}}
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return result, nil
	}
	var input struct {
		Type             string            `json:"type"`
		InlineAgents     json.RawMessage   `json:"inline_agents"`
		PredefinedAgents []json.RawMessage `json:"predefined_agents,omitzero"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return cmaDelegation{}, errors.New("setting must be an object")
	}
	if input.Type == "disabled" {
		return cmaDelegation{Type: "disabled"}, nil
	}
	if input.Type != "enabled" {
		return cmaDelegation{}, errors.New("type must be enabled or disabled")
	}
	if len(input.InlineAgents) > 0 && !httpapi.IsJSONNull(input.InlineAgents) {
		var inline cmaSwitch
		if err := json.Unmarshal(input.InlineAgents, &inline); err != nil {
			return cmaDelegation{}, errors.New("inline_agents must be an object")
		}
		if inline.Type != "enabled" && inline.Type != "disabled" {
			return cmaDelegation{}, errors.New("inline_agents.type must be enabled or disabled")
		}
		result.InlineAgents = &inline
	}
	if len(input.PredefinedAgents) > 20 {
		return cmaDelegation{}, errors.New("predefined_agents must contain at most 20 entries")
	}
	seen := make(map[string]bool, len(input.PredefinedAgents))
	for _, item := range input.PredefinedAgents {
		ref, _, err := h.resolveRosterEntry(r, principal, selfID, selfVersion, item)
		if err != nil {
			return cmaDelegation{}, err
		}
		if seen[ref.ID] {
			return cmaDelegation{}, errors.New("predefined_agents must reference distinct agents")
		}
		seen[ref.ID] = true
		result.PredefinedAgents = append(result.PredefinedAgents, ref)
	}
	if result.InlineAgents.Type == "disabled" && len(result.PredefinedAgents) == 0 {
		return cmaDelegation{}, errors.New("predefined_agents must not be empty when inline_agents is disabled")
	}
	return result, nil
}

func normalizeCMAAdvisor(raw json.RawMessage) (cmaAdvisor, error) {
	var result cmaAdvisor
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return cmaAdvisor{Type: "disabled"}, nil
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return cmaAdvisor{}, errors.New("multiagent.advisor must be an object")
	}
	switch result.Type {
	case "disabled":
		result.Model = ""
	case "enabled":
		if result.Model == "" {
			return cmaAdvisor{}, errors.New("multiagent.advisor.model is required")
		}
	default:
		return cmaAdvisor{}, errors.New("multiagent.advisor.type must be enabled or disabled")
	}
	return result, nil
}

func mergeCMAMultiagent(stored, patch json.RawMessage) (json.RawMessage, error) {
	var current, update map[string]json.RawMessage
	if err := json.Unmarshal(patch, &update); err != nil {
		return nil, err
	}
	if update == nil {
		return patch, nil
	}
	if err := json.Unmarshal(stored, &current); err != nil {
		return patch, nil
	}
	if string(current["type"]) != `"multiagent_20261001"` || string(update["type"]) != `"multiagent_20261001"` {
		return patch, nil
	}
	for _, name := range []string{"subagents", "workflows"} {
		value, present := update[name]
		if !present {
			continue
		}
		var previous, next map[string]json.RawMessage
		if json.Unmarshal(current[name], &previous) == nil && json.Unmarshal(value, &next) == nil && next != nil && string(next["type"]) == `"enabled"` && string(previous["type"]) == `"enabled"` {
			for _, field := range []string{"inline_agents", "predefined_agents"} {
				if _, exists := next[field]; !exists {
					next[field] = previous[field]
				}
			}
			encoded, err := jsonx.Encode(next)
			if err != nil {
				return nil, err
			}
			update[name] = encoded
		}
	}
	for name, value := range update {
		current[name] = value
	}
	return jsonx.Encode(current)
}
