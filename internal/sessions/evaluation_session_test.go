package sessions

import (
	"encoding/json"
	"testing"
)

func TestEvaluationSessionRequestUsesAgentVersionAndQuery(t *testing.T) {
	request, err := evaluationSessionRequest(EvaluationSessionSpec{
		AgentExternalID:       "agent_demo",
		AgentVersion:          3,
		EnvironmentExternalID: "env_demo",
		Query:                 "汇总销售额",
		Title:                 "评测 · 销售额",
		VaultIDs:              []string{"vault_demo"},
		Metadata:              map[string]string{"evaluation_task_id": "evaltask_demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var agent struct {
		Type    string `json:"type"`
		ID      string `json:"id"`
		Version int    `json:"version"`
		System  string `json:"system"`
	}
	if err := json.Unmarshal(request.Agent, &agent); err != nil {
		t.Fatal(err)
	}
	if agent.Type != "agent" || agent.ID != "agent_demo" || agent.Version != 3 || agent.System != "" {
		t.Fatalf("agent = %+v", agent)
	}
	var events []struct {
		Type    string `json:"type"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(request.InitialEvents, &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "user.message" || events[0].Content[0].Text != "汇总销售额" {
		t.Fatalf("events = %+v", events)
	}
	if len(request.Title) == 0 {
		t.Fatal("missing title")
	}
	var vaults []string
	if err := json.Unmarshal(request.VaultIDs, &vaults); err != nil {
		t.Fatal(err)
	}
	if len(vaults) != 1 || vaults[0] != "vault_demo" {
		t.Fatalf("vaults = %+v", vaults)
	}
}
