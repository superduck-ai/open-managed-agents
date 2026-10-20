//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAgentsCMAMultiagentConfiguration(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("agents-cma-multiagent"))
	child := createAgent(t, app, `{"name":"saved-worker","model":"claude-sonnet-4-6"}`)
	invalid := doAgentRequest(t, app, http.MethodPost, "/v1/agents", strings.NewReader(`{"name":"invalid","model":"claude-sonnet-4-6","multiagent":{"type":"multiagent_20261001","subagents":{"type":"enabled","inline_agents":{"type":"disabled"},"predefined_agents":[]}}}`), defaultTestKey, true)
	invalid.Body.Close()
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid configuration status = %d", invalid.StatusCode)
	}
	created := createAgent(t, app, `{"name":"console-agent","model":"claude-sonnet-4-6","multiagent":{"type":"multiagent_20261001","workflows":{"type":"enabled","inline_agents":{"type":"enabled"},"predefined_agents":[]},"subagents":{"type":"enabled","inline_agents":{"type":"disabled"},"predefined_agents":[`+quoteJSON(child.ID)+`]},"advisor":{"type":"enabled","model":"claude-haiku-5-5"}}}`)
	updateAgent(t, app, child.ID, `{"version":1,"system":"new version"}`, http.StatusOK)
	updated := updateAgent(t, app, created.ID, `{"version":1,"multiagent":{"type":"multiagent_20261001","subagents":{"type":"enabled"},"workflows":{"type":"disabled"}}}`, http.StatusOK)
	var config struct {
		Type      string `json:"type"`
		Subagents struct {
			PredefinedAgents []struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"predefined_agents"`
		} `json:"subagents"`
		Workflows struct {
			Type string `json:"type"`
		} `json:"workflows"`
		Advisor struct {
			Model string `json:"model"`
		} `json:"advisor"`
	}
	if err := json.Unmarshal(updated.Multiagent, &config); err != nil {
		t.Fatal(err)
	}
	if config.Type != "multiagent_20261001" || config.Workflows.Type != "disabled" || config.Advisor.Model != "claude-haiku-5-5" || len(config.Subagents.PredefinedAgents) != 1 || config.Subagents.PredefinedAgents[0].Version != 1 || config.Subagents.PredefinedAgents[0].ID != child.ID {
		t.Fatalf("updated = %s", updated.Multiagent)
	}
	saved := retrieveAgent(t, app, created.ID, "")
	if string(saved.Multiagent) != string(updated.Multiagent) {
		t.Fatalf("readback = %s, want %s", saved.Multiagent, updated.Multiagent)
	}
	reset := updateAgent(t, app, created.ID, `{"version":2,"multiagent":{"type":"multiagent_20261001","advisor":null,"subagents":null}}`, http.StatusOK)
	assertRawContains(t, reset.Multiagent, `"type":"enabled"`)
	assertRawContains(t, reset.Multiagent, `"advisor":{"type":"disabled"}`)
}
