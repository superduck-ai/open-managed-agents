//go:build integration

package tests

import (
	"encoding/json"
	"testing"
)

func TestBrainUserQuestionWaitsForCustomAnswer(t *testing.T) {
	for _, scenario := range []struct {
		name, policy string
		failed       bool
	}{
		{"allow", "always_allow", false},
		{"ask", "always_ask", false},
		{"error", "always_allow", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			policy := scenario.policy
			app := newPayloadIntegrationApp(t, newFakeStore("brain-user-question"))
			worker, epoch := newPayloadIntegrationSessionWithAgent(t, app, `{"model":"claude-opus-4-6","name":"questions","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"ask_user_question","enabled":true,"permission_policy":{"type":`+quoteJSON(policy)+`}}]}]}`)
			postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
				`{"type":"control_request","uuid":"ask","request_id":"ask","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"question-call","input":{"questions":[{"id":"colors","question":"Which colors?","multi_select":true,"options":[{"label":"Blue"},{"label":"Red"}]}]}}}`))
			calls := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.custom_tool_use", defaultTestKey)
			if len(calls.Data) != 1 {
				t.Fatalf("question calls = %s", calls.Data)
			}
			call := sessionEventObjectByType(t, calls, "agent.custom_tool_use")
			callID := call["id"].(string)
			record, found, err := app.db.GetCodeSession(t.Context(), worker.ExternalID)
			if err != nil || !found {
				t.Fatal("missing code session", err)
			}
			if string(record.WorkerExternalMetadata) == "" {
				t.Fatal("question wait was not registered")
			}
			statuses := listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.status_idle", defaultTestKey)
			status := sessionEventObjectByType(t, statuses, "session.status_idle")
			stop := status["stop_reason"].(map[string]any)
			if stop["type"] != "requires_action" || stringArrayField(stop, "event_ids")[0] != callID {
				t.Fatalf("question did not wait for the user: %v", status)
			}
			answer := `{"answers":[{"id":"colors","selected":["Blue"],"custom":"Green"}]}`
			result := `"content":[{"type":"text","text":` + quoteJSON(answer) + `}]`
			if scenario.failed {
				result = `"is_error":true`
			}
			sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.custom_tool_result","custom_tool_use_id":`+quoteJSON(callID)+`,`+result+`}]}`, defaultTestKey)
			if len(sent.Data) != 1 {
				t.Fatal("answer not accepted")
			}
			_, payload := latestCodeSessionControlResponse(t, app, worker.ExternalID)
			var response struct {
				Response struct {
					RequestID string `json:"request_id"`
					Response  struct {
						Behavior       string `json:"behavior"`
						ExternalResult *struct {
							IsError bool `json:"isError"`
							Content []struct {
								Type string `json:"type"`
								Text string `json:"text"`
							} `json:"content"`
						} `json:"externalResult"`
					} `json:"response"`
				} `json:"response"`
			}
			if err := json.Unmarshal(payload, &response); err != nil {
				t.Fatal(err)
			}
			decision := response.Response.Response
			if response.Response.RequestID != "ask" || decision.Behavior != "allow" {
				t.Fatalf("answer response = %s", payload)
			}
			if decision.ExternalResult == nil || decision.ExternalResult.IsError != scenario.failed || len(decision.ExternalResult.Content) != 1 {
				t.Fatalf("custom result = %s", payload)
			}
			if !scenario.failed && decision.ExternalResult.Content[0].Text != answer {
				t.Fatalf("answer content changed: %s", payload)
			}
		})
	}
}
