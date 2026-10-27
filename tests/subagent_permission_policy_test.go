//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestSubagentUsesOwnToolPermissionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, childPolicy, parentPolicy, want string
		childDisabled                         bool
		modelFirst                            bool
	}{
		{"child disabled parent allow", "always_allow", "always_allow", "deny", true, false},
		{"child ask parent allow", "always_ask", "always_allow", "ask", false, false},
		{"child allow parent ask", "always_allow", "always_ask", "allow", false, false},
		{"model first child disabled parent allow", "always_allow", "always_allow", "deny", true, true},
		{"model first child ask parent allow", "always_ask", "always_allow", "ask", false, true},
		{"model first child allow parent ask", "always_allow", "always_ask", "allow", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("subagent-own-policy"))
			enabled := "true"
			if tc.childDisabled {
				enabled = "false"
			}
			child := createAgent(t, app, `{"model":"claude-opus-4-6","name":"reviewer","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","enabled":`+enabled+`,"permission_policy":{"type":`+quoteJSON(tc.childPolicy)+`}}]}]}`)
			parent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"parent","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","permission_policy":{"type":`+quoteJSON(tc.parentPolicy)+`}}]}],"multiagent":{"type":"coordinator","agents":[{"type":"agent","id":`+quoteJSON(child.ID)+`}]}}`)
			env := createEnvironment(t, app, `{"name":"subagent-own-policy"}`)
			session := createSession(t, app, `{"agent":`+quoteJSON(parent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			codeID := launchLocalCodeSession(t, app, session.ID)
			epoch := registerCodeSessionWorker(t, app, codeID)
			if tc.modelFirst {
				snapshot, err := json.Marshal(child)
				if err != nil {
					t.Fatal(err)
				}
				postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch, `{"type":"session.thread_created","uuid":"early-child","session_thread_id":"sthr_policy","agent_name":"reviewer","agent":`+string(snapshot)+`}`))
				code, _, err := app.db.GetCodeSession(t.Context(), codeID)
				if err != nil {
					t.Fatal(err)
				}
				service := newCodeSessionService(app, nil, nil)
				service.SetPublicEventSink(sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil))
				request, err := service.BeginModelRequest(t.Context(), code.WorkspaceUUID, session.ID, codeID, "sthr_policy", "claude-opus-4-6")
				if err != nil {
					t.Fatal(err)
				}
				if err := service.EndModelRequest(t.Context(), request, codesessions.ModelRequestResult{EndedAt: time.Now(), ToolUseIDs: []string{"child-write"}, ToolUses: []codesessions.ModelRequestToolUse{{ID: "child-write", Name: "Write", Input: json.RawMessage(`{"file_path":"/tmp/policy"}`)}}}); err != nil {
					t.Fatal(err)
				}
				early := listThreadEvents(t, app, session.ID, "sthr_policy", defaultTestKey)
				count := 0
				for _, event := range early.Data {
					if sessionEventStringField(t, event, "type") == "agent.tool_use" {
						count++
					}
				}
				want := 0
				if tc.want == "allow" {
					want = 1
				}
				if count != want {
					t.Fatalf("model first published %d tool events, want %d for child %s", count, want, tc.want)
				}
			}
			requestSubagentWrite(t, app, codeID, epoch, "policy", "child-write")
			tool := sessionEventObjectByType(t, listThreadEvents(t, app, session.ID, "sthr_policy", defaultTestKey), "agent.tool_use")
			if tool["evaluated_permission"] != tc.want {
				t.Fatalf("child permission = %v, want %s", tool["evaluated_permission"], tc.want)
			}
			_, raw := latestCodeSessionControlResponse(t, app, codeID)
			var reply struct {
				Response struct {
					RequestID string `json:"request_id"`
				} `json:"response"`
			}
			if err := json.Unmarshal(raw, &reply); err != nil {
				t.Fatal(err)
			}
			if tc.want == "ask" {
				if reply.Response.RequestID == "write-request-policy" {
					t.Fatal("child ask received an automatic decision")
				}
				sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(tool["id"].(string))+`,"result":"allow"}]}`, defaultTestKey)
				confirmation := sessionEventObjectByType(t, listSessionEvents(t, app, session.ID, "order=asc", defaultTestKey), "user.tool_confirmation")
				if confirmation["tool_use_id"] != tool["id"] || confirmation["result"] != "allow" {
					t.Fatalf("public confirmation lost its target or decision: %v", confirmation)
				}
				_, raw = latestCodeSessionControlResponse(t, app, codeID)
				if err := json.Unmarshal(raw, &reply); err != nil || reply.Response.RequestID != "write-request-policy" {
					t.Fatalf("confirmation routed incorrectly: %s %v", raw, err)
				}
			} else if reply.Response.RequestID != "write-request-policy" {
				t.Fatalf("missing child automatic decision: %s", raw)
			}
			postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch, `{"type":"control_request","uuid":"primary-write","request_id":"primary-write-request","request":{"subtype":"can_use_tool","tool_name":"Write","tool_use_id":"primary-write","input":{"file_path":"/tmp/primary"}}}`))
			primary := sessionEventObjectByType(t, listSessionEvents(t, app, session.ID, "order=desc", defaultTestKey), "agent.tool_use")
			wantParent := "allow"
			if tc.parentPolicy == "always_ask" {
				wantParent = "ask"
			}
			if primary["evaluated_permission"] != wantParent {
				t.Fatalf("primary policy changed: %v", primary)
			}
		})
	}
}
