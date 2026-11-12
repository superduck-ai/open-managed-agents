package sessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestThreadResponseProjectsAgentToolConfigTypesWithoutRewritingSnapshot(t *testing.T) {
	raw := json.RawMessage(`{"id":"agent-child","version":3,"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"read","enabled":true,"permission_policy":{"type":"always_ask"}}]}]}`)
	record := db.SessionThread{AgentSnapshot: raw}
	response := responseFromThread(record)
	var agent struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
		Tools   []struct {
			Configs []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"configs"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(response.Agent, &agent); err != nil {
		t.Fatal(err)
	}
	if agent.ID != "agent-child" || agent.Version != 3 || agent.Tools[0].Configs[0].Type != "read" {
		t.Fatal("snapshot public schema", agent)
	}
	if string(record.AgentSnapshot) != string(raw) {
		t.Fatal("rewrote durable snapshot")
	}
}
