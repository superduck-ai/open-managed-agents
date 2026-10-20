package agents

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestCMAMultiagentRejectsInvalidNestedConfiguration(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	r := httptest.NewRequest("POST", "/v1/agents", nil)
	for _, raw := range []string{
		`{"type":"multiagent_20261001","subagents":{}}`,
		`{"type":"multiagent_20261001","subagents":{"type":"unknown"}}`,
		`{"type":"multiagent_20261001","workflows":{"type":"enabled","inline_agents":{"type":"disabled"}}}`,
		`{"type":"multiagent_20261001","subagents":{"type":"enabled","inline_agents":{}}}`,
		`{"type":"multiagent_20261001","advisor":{"type":"enabled"}}`,
		`{"type":"multiagent_20261001","advisor":{}}`,
	} {
		if _, err := h.normalizeMultiagent(r, auth.Principal{}, "self", 1, json.RawMessage(raw)); err == nil {
			t.Errorf("accepted invalid configuration %s", raw)
		}
	}
}

func TestCMAMultiagentDefaultsAndIndependentSwitches(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	r := httptest.NewRequest("POST", "/v1/agents", nil)
	raw, err := h.normalizeMultiagent(r, auth.Principal{}, "self", 1, json.RawMessage(`{"type":"multiagent_20261001"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got cmaMultiagent
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "multiagent_20261001" || got.Subagents.Type != "enabled" || got.Workflows.Type != "enabled" || got.Subagents.InlineAgents.Type != "enabled" || got.Workflows.InlineAgents.Type != "enabled" || got.Advisor.Type != "disabled" {
		t.Fatalf("defaults %s", raw)
	}
	raw, err = h.normalizeMultiagent(r, auth.Principal{}, "self", 1, json.RawMessage(`{"type":"multiagent_20261001","workflows":{"type":"disabled"},"subagents":{"type":"enabled","inline_agents":{"type":"disabled"},"predefined_agents":[{"type":"self"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Workflows.Type != "disabled" || got.Subagents.InlineAgents.Type != "disabled" || len(got.Subagents.PredefinedAgents) != 1 || got.Subagents.PredefinedAgents[0].ID != "self" || got.Subagents.PredefinedAgents[0].Version != 1 {
		t.Fatalf("independent settings %s", raw)
	}
}

func TestCMAMultiagentUpdatePreservesFieldsAndResetsDefaults(t *testing.T) {
	stored := json.RawMessage(`{"type":"multiagent_20261001","subagents":{"type":"enabled","inline_agents":{"type":"disabled"},"predefined_agents":[{"type":"self"}]},"workflows":{"type":"disabled"},"advisor":{"type":"enabled","model":"claude-haiku-5-5"}}`)
	h := NewHandler(nil, nil, nil)
	r := httptest.NewRequest("POST", "/v1/agents", nil)
	for _, tc := range []struct {
		patch     string
		inline    string
		workflows string
		advisor   string
		agents    int
	}{
		{`{"type":"multiagent_20261001","subagents":{"type":"enabled"}}`, "disabled", "disabled", "enabled", 1},
		{`{"type":"multiagent_20261001","subagents":null,"workflows":null,"advisor":null}`, "enabled", "enabled", "disabled", 0},
	} {
		merged, err := mergeCMAMultiagent(stored, json.RawMessage(tc.patch))
		if err != nil {
			t.Fatal(err)
		}
		normalized, err := h.normalizeMultiagent(r, auth.Principal{}, "self", 2, merged)
		if err != nil {
			t.Fatal(err)
		}
		var got cmaMultiagent
		if err := json.Unmarshal(normalized, &got); err != nil {
			t.Fatal(err)
		}
		if got.Subagents.InlineAgents.Type != tc.inline || got.Workflows.Type != tc.workflows || got.Advisor.Type != tc.advisor || len(got.Subagents.PredefinedAgents) != tc.agents {
			t.Fatalf("patch %s: %s", tc.patch, normalized)
		}
	}
	cleared, err := mergeCMAMultiagent(stored, json.RawMessage(`null`))
	if err != nil || string(cleared) != "null" {
		t.Fatalf("clear = %s, %v", cleared, err)
	}
}

func TestCMAMultiagentConsoleConfiguration(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	r := httptest.NewRequest("POST", "/v1/agents", nil)
	raw := json.RawMessage(`{"type":"multiagent_20261001","workflows":{"type":"enabled","inline_agents":{"type":"enabled"},"predefined_agents":[]},"subagents":{"type":"enabled","inline_agents":{"type":"enabled"},"predefined_agents":[]},"advisor":{"type":"enabled","model":"claude-haiku-5-5"}}`)
	normalized, err := h.normalizeMultiagent(r, auth.Principal{}, "self", 1, raw)
	if err != nil {
		t.Fatal(err)
	}
	var got cmaMultiagent
	if err := json.Unmarshal(normalized, &got); err != nil {
		t.Fatal(err)
	}
	if got.Advisor.Model != "claude-haiku-5-5" || got.Subagents.PredefinedAgents == nil || got.Workflows.PredefinedAgents == nil {
		t.Fatalf("configuration lost: %s", normalized)
	}
}
