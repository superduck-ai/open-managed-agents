package sessions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluationCaseSessionRequestBuildsManagedSession(t *testing.T) {
	request, err := evaluationCaseSessionRequest(EvaluationCaseSessionSpec{
		AgentExternalID:       "agent_test",
		AgentVersion:          4,
		EnvironmentExternalID: "env_test",
		Resources:             json.RawMessage(`[{"type":"file","file_id":"file_test"}]`),
		Turns:                 []string{"hello", "again"},
		Metadata:              map[string]string{"experiment_id": "evalexp_test", "case_id": "evalcase_test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(request.Agent), `"version":4`) || !strings.Contains(string(request.Agent), `"id":"agent_test"`) {
		t.Fatalf("agent = %s", request.Agent)
	}
	if string(request.EnvironmentID) != `"env_test"` {
		t.Fatalf("environment = %s", request.EnvironmentID)
	}
	if string(request.Resources) != `[{"type":"file","file_id":"file_test"}]` {
		t.Fatalf("resources = %s", request.Resources)
	}
	var events []map[string]any
	if err := json.Unmarshal(request.InitialEvents, &events); err != nil || len(events) != 2 || events[0]["type"] != "user.message" {
		t.Fatalf("events = %s err=%v", request.InitialEvents, err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(request.Metadata, &metadata); err != nil || metadata["experiment_id"] != "evalexp_test" || metadata["case_id"] != "evalcase_test" {
		t.Fatalf("metadata = %s err=%v", request.Metadata, err)
	}
}

func TestEvaluationCaseSessionRequestOmitsEmptyResources(t *testing.T) {
	request, err := evaluationCaseSessionRequest(EvaluationCaseSessionSpec{
		AgentExternalID: "agent_test", AgentVersion: 1, EnvironmentExternalID: "env_test", Turns: []string{"hello"},
	})
	if err != nil || len(request.Resources) != 0 || !strings.Contains(string(request.Metadata), "{}") {
		t.Fatalf("request = %#v err=%v", request, err)
	}
}
