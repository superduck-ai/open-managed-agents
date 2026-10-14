package tests

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestBuiltinToolProxyPublishesWithoutPermissionCallback(t *testing.T) {
	for _, streaming := range []bool{true, false} {
		t.Run(fmt.Sprintf("stream=%t", streaming), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_tool\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_bash\",\"name\":\"Bash\",\"input\":{}}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\":\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"printf hello\\\"}\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
					return
				}
				_, _ = io.WriteString(w, `{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_bash","name":"Bash","input":{"command":"printf hello"}}]}`)
			}))
			defer upstream.Close()
			app := newPayloadIntegrationApp(t, newFakeStore("builtin-proxy"))
			clearTestLLMProviders(t, app)
			seedTestLLMProvider(t, app, "Builtin", upstream.URL, "provider-key", messagesTestModel)
			agent := createAgent(t, app, `{"name":"builtin-proxy","model":"`+messagesTestModel+`","tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}}]}`)
			environment := createEnvironment(t, app, `{"name":"builtin-proxy"}`)
			createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
			credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
			epoch := registerCodeSessionWorker(t, app, credential.CodeSessionID)
			record, found, err := app.db.GetCodeSession(t.Context(), credential.CodeSessionID)
			if err != nil || !found {
				t.Fatalf("code session: %v", err)
			}
			response := doMessagesRequest(t, app, credential.Token, fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[]}`, messagesTestModel, streaming))
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("proxy: status=%d error=%v", response.StatusCode, err)
			}
			history := listSessionEvents(t, app, record.SessionExternalID, "types[]=agent.tool_use", defaultTestKey)
			if len(history.Data) != 1 {
				t.Fatalf("tool calls without permission callback = %d, want 1", len(history.Data))
			}
			tool := sessionEventObjectByType(t, history, "agent.tool_use")
			if tool["name"] != "bash" || tool["evaluated_permission"] != "allow" || tool["input"].(map[string]any)["command"] != "printf hello" {
				t.Fatalf("tool event = %#v", tool)
			}
			if tool["evaluation"].(map[string]any)["type"] != "always_allow" {
				t.Fatalf("tool policy evaluation = %#v", tool["evaluation"])
			}
			postCodeSessionWorkerEvents(t, app, record.ExternalID, internalPayloadRequest(epoch, `{"type":"user","uuid":"bash-result","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"hello"}]}}`))
			results := listSessionEvents(t, app, record.SessionExternalID, "types[]=agent.tool_result", defaultTestKey)
			if result := sessionEventObjectByType(t, results, "agent.tool_result"); result["tool_use_id"] != tool["id"] {
				t.Fatalf("tool result does not reference published call: %#v", result)
			}
			postCodeSessionWorkerEvents(t, app, record.ExternalID, internalPayloadRequest(epoch, `{"type":"control_request","uuid":"bash-permission","request_id":"bash-permission","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"toolu_bash","input":{"command":"printf hello"}}}`))
			if calls := listSessionEvents(t, app, record.SessionExternalID, "types[]=agent.tool_use", defaultTestKey); len(calls.Data) != 1 {
				t.Fatalf("permission callback duplicated tool event: %s", calls.Data)
			}
		})
	}
}

func TestBuiltinToolIntermediateIdleDoesNotEndTurn(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("builtin-idle"))
	record, epoch := newPayloadIntegrationSession(t, app)
	workerState := func(status string) {
		putCodeSessionWorkerState(t, app, record.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":%q}`, epoch, status))
	}
	workerState("running")
	service := newCodeSessionService(app, nil, nil)
	service.SetPublicEventSink(sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil))
	for index, tools := range [][]string{{"tool_write"}, {"tool_read"}, nil} {
		request, err := service.BeginModelRequest(t.Context(), record.WorkspaceUUID, record.SessionExternalID, record.ExternalID, "", "model-mock")
		if err != nil {
			t.Fatal(err)
		}
		if err := service.EndModelRequest(t.Context(), request, codesessions.ModelRequestResult{EndedAt: time.Now().UTC(), ToolUseIDs: tools}); err != nil {
			t.Fatal(err)
		}
		if index < 2 {
			workerState("requires_action")
			if status := retrieveSession(t, app, record.SessionExternalID, defaultTestKey).Status; status != "running" {
				t.Fatalf("automatic tool step %d paused turn: %s", index, status)
			}
		}
		workerState("idle")
		if index < 2 {
			if status := retrieveSession(t, app, record.SessionExternalID, defaultTestKey).Status; status != "running" {
				t.Fatalf("tool step %d ended turn: %s", index, status)
			}
			if idle := listSessionEvents(t, app, record.SessionExternalID, "types[]=session.status_idle", defaultTestKey); len(idle.Data) != 0 {
				t.Fatalf("tool step %d published premature idle: %s", index, idle.Data)
			}
		}
	}
	if status := retrieveSession(t, app, record.SessionExternalID, defaultTestKey).Status; status != "idle" {
		t.Fatalf("completed chain status = %s", status)
	}
	idle := listSessionEvents(t, app, record.SessionExternalID, "types[]=session.status_idle", defaultTestKey)
	if len(idle.Data) != 1 || !strings.Contains(string(idle.Data[0]), "end_turn") {
		t.Fatalf("completed chain idle = %s", idle.Data)
	}
}
