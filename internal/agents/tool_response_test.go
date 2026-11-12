package agents

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestAgentResponseInfersMissingBuiltinConfigTypesForExistingVersions(t *testing.T) {
	raw := json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true},{"name":"ask_user_question","enabled":false}]}]`)
	record := db.Agent{ExternalID: "agent-owned", CurrentVersion: 7, Tools: raw}
	response := responseFromAgent(record)
	var tools []struct {
		Configs []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"configs"`
	}
	if err := json.Unmarshal(response.Tools, &tools); err != nil {
		t.Fatal(err)
	}
	for _, config := range tools[0].Configs {
		if config.Type != config.Name {
			t.Fatal("response lacks formal discriminator", config)
		}
	}
	if response.Version != 7 || response.ID != "agent-owned" || string(record.Tools) != string(raw) {
		t.Fatal("mutated existing version")
	}
}
