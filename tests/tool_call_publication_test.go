//go:build integration || e2e || e2b_integration

package tests

import (
	"fmt"
	"testing"
)

func TestToolAttemptsPublishLinkedPairsWithoutDuplicateCalls(t *testing.T) {
	for _, tc := range []struct {
		name, tool, multiagent, permission string
		unavailable, failed                bool
	}{
		{"unknown", "unknown_tool", "null", "deny", true, true},
		{"unavailable", "Bash", "null", "deny", true, true},
		{"delegation disabled", "list_agents", `{"type":"multiagent_20261001","subagents":{"type":"disabled"}}`, "deny", false, true},
		{"invalid bash arguments", "Bash", "null", "allow", false, true},
		{"internal list", "list_agents", `{"type":"multiagent_20261001"}`, "allow", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("tool-publication"))
			worker, epoch := newPayloadIntegrationSessionWithAgent(t, app, `{"model":"claude-opus-4-6","name":"tool-publication","multiagent":`+tc.multiagent+`}`)
			available := ""
			if tc.unavailable {
				available = `,"tool_available":false`
			}
			input := internalPayloadRequest(epoch,
				`{"type":"control_request","uuid":"attempt","request_id":"attempt","request":{"subtype":"can_use_tool","tool_name":`+quoteJSON(tc.tool)+`,"tool_use_id":"call","input":{}`+available+`}}`,
				fmt.Sprintf(`{"type":"user","uuid":"result","message":{"content":[{"type":"tool_result","tool_name":%s,"tool_use_id":"call","is_error":%t,"content":"observed result"}]}}`, quoteJSON(tc.tool), tc.failed))
			for range 2 {
				postCodeSessionWorkerEvents(t, app, worker.ExternalID, input)
			}
			calls := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.tool_use", defaultTestKey)
			results := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.tool_result", defaultTestKey)
			if len(calls.Data) != 1 || len(results.Data) != 1 {
				t.Fatalf("calls=%s results=%s", calls.Data, results.Data)
			}
			call := sessionEventObjectByType(t, calls, "agent.tool_use")
			result := sessionEventObjectByType(t, results, "agent.tool_result")
			if call["evaluated_permission"] != tc.permission || result["tool_use_id"] != call["id"] || result["is_error"] != tc.failed {
				t.Fatalf("call=%v result=%v", call, result)
			}
			if tc.permission == "deny" && call["evaluation"] != nil {
				t.Fatalf("invented evaluation: %v", call)
			}
		})
	}
}

const bashAskAgentBody = `{"model":"claude-opus-4-6","name":"bash-ask-fixture","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"bash","permission_policy":{"type":"always_ask"}}]}]}`
