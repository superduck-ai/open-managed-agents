//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
	"net/http"
	"strings"
	"testing"
)

func subagentApprovalFixture(t *testing.T) (*testApp, string, string, string) {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("subagent-identities"))
	child := createAgent(t, app, `{"model":"claude-opus-4-6","name":"reviewer","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","permission_policy":{"type":"always_ask"}}]}]}`)
	parent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"parent","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","permission_policy":{"type":"always_ask"}}]}],"multiagent":{"type":"coordinator","agents":[{"type":"agent","id":`+quoteJSON(child.ID)+`}]}}`)
	env := createEnvironment(t, app, `{"name":"subagent-identities"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(parent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, codeID)
	return app, session.ID, codeID, epoch
}

func requestSubagentWrite(t *testing.T, app *testApp, codeID, epoch, label, providerID string) {
	t.Helper()
	code, found, err := app.db.GetCodeSession(t.Context(), codeID)
	if err != nil || !found {
		t.Fatalf("load code session: %v", err)
	}
	session, found, err := app.db.GetSession(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("load session: %v", err)
	}
	var parent struct {
		Multiagent struct {
			Agents []struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"agents"`
		} `json:"multiagent"`
	}
	if err := json.Unmarshal(session.AgentSnapshot, &parent); err != nil || len(parent.Multiagent.Agents) != 1 {
		t.Fatalf("load pinned child reference: %s %v", session.AgentSnapshot, err)
	}
	ref := parent.Multiagent.Agents[0]
	child := retrieveAgent(t, app, ref.ID, fmt.Sprintf("version=%d", ref.Version))
	snapshot, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch,
		`{"type":"control_request","uuid":"dispatch-`+label+`","request_id":"dispatch-request-`+label+`","request":{"subtype":"can_use_tool","tool_name":"Agent","tool_use_id":"dispatch-tool-`+label+`","input":{"subagent_type":"reviewer"}}}`,
		`{"type":"session.thread_created","uuid":"child-created-`+label+`","session_thread_id":"sthr_`+label+`","agent_name":"reviewer","agent":`+string(snapshot)+`,"task_id":"runtime-`+label+`","tool_use_id":"dispatch-tool-`+label+`"}`,
		`{"type":"control_request","uuid":"write-`+label+`","session_thread_id":"sthr_`+label+`","request_id":"write-request-`+label+`","request":{"subtype":"can_use_tool","tool_name":"Write","tool_use_id":`+quoteJSON(providerID)+`,"input":{"file_path":"/tmp/`+label+`"}}}`))
}

func TestSubagentToolIDsAreScopedToInstances(t *testing.T) {
	app, sessionID, codeID, epoch := subagentApprovalFixture(t)
	requestSubagentWrite(t, app, codeID, epoch, "first", "toolu_functions.Write:0")
	first := sessionEventObjectByType(t, listThreadEvents(t, app, sessionID, "sthr_first", defaultTestKey), "agent.tool_use")
	sendSessionEvents(t, app, sessionID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(first["id"].(string))+`,"result":"deny","session_thread_id":"sthr_first"}]}`, defaultTestKey)
	requestSubagentWrite(t, app, codeID, epoch, "second", "toolu_functions.Write:0")
	second := listThreadEvents(t, app, sessionID, "sthr_second", defaultTestKey)
	secondTool := sessionEventObjectByType(t, second, "agent.tool_use")
	if secondTool["id"] == first["id"] || secondTool["primary_event_id"] == first["primary_event_id"] {
		t.Fatal("different instances reused original or mirror tool ID")
	}
	requestSubagentWrite(t, app, codeID, epoch, "third", "toolu_functions.Write:0")
	third := sessionEventObjectByType(t, listThreadEvents(t, app, sessionID, "sthr_third", defaultTestKey), "agent.tool_use")
	if secondTool["id"] == third["id"] {
		t.Fatal("overlapping child calls share an ID")
	}
	invalid := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(first["primary_event_id"].(string))+`,"result":"allow","session_thread_id":"sthr_second"}]}`), defaultTestKey, true)
	assertError(t, invalid, http.StatusConflict, "conflict_error")
	for _, approval := range []struct{ thread, id string }{
		{"sthr_second", secondTool["primary_event_id"].(string)},
		{"sthr_third", third["id"].(string)},
	} {
		sendSessionEvents(t, app, sessionID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(approval.id)+`,"result":"allow","session_thread_id":`+quoteJSON(approval.thread)+`}]}`, defaultTestKey)
		_, raw := latestCodeSessionControlResponse(t, app, codeID)
		var response struct {
			Response struct {
				RequestID string `json:"request_id"`
			} `json:"response"`
		}
		if err := jsonv2.Unmarshal(raw, &response); err != nil || response.Response.RequestID != "write-request-"+strings.TrimPrefix(approval.thread, "sthr_") {
			t.Fatalf("wrong runtime approval response: %s %v", raw, err)
		}
	}
	postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch, `{"type":"user","uuid":"second-result","session_thread_id":"sthr_second","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_functions.Write:0","content":"done"}]}}`))
	result := sessionEventObjectByType(t, listThreadEvents(t, app, sessionID, "sthr_second", defaultTestKey), "agent.tool_result")
	if result["tool_use_id"] != secondTool["id"] {
		t.Fatalf("result linked wrong child: %v", result)
	}
}

func TestSubagentThreadMappingsAcrossPages(t *testing.T) {
	app, sessionID, codeID, _ := subagentApprovalFixture(t)
	worker, found, err := app.db.GetCodeSession(t.Context(), codeID)
	if err != nil || !found {
		t.Fatalf("load worker: found=%t error=%v", found, err)
	}
	service := newCodeSessionService(app, nil, nil)
	sink := sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil)
	service.SetPublicEventSink(sink)
	payloads := make([]json.RawMessage, 501)
	for i := range payloads {
		payloads[i] = json.RawMessage(fmt.Sprintf(`{"type":"session.thread_created","id":"sevt_mapping_%d","session_thread_id":"sthr_original","agent_name":"reviewer","task_id":"task_%d","agent_id":"runtime_child","agentId":"native_alias","tool_use_id":"agent_call"}`, i, i))
	}
	if err := sink.PublishCodeSessionEvents(t.Context(), worker, payloads); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"task_500", "runtime_child", "native_alias", "agent_call"} {
		request, err := service.BeginModelRequest(t.Context(), worker.WorkspaceUUID, sessionID, codeID, id, "model-mock")
		if err != nil {
			t.Fatal(err)
		}
		if request.ThreadID != "sthr_original" {
			t.Fatalf("instance %s routed to %s", id, request.ThreadID)
		}
		if err := service.EndModelRequest(t.Context(), request, codesessions.ModelRequestResult{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTerminatedSubagentRejectsApproval(t *testing.T) {
	app, sessionID, codeID, epoch := subagentApprovalFixture(t)
	requestSubagentWrite(t, app, codeID, epoch, "cancel", "unique-cancel-write")
	tool := sessionEventObjectByType(t, listThreadEvents(t, app, sessionID, "sthr_cancel", defaultTestKey), "agent.tool_use")
	requestSubagentWrite(t, app, codeID, epoch, "live", "unique-live-write")
	live := sessionEventObjectByType(t, listThreadEvents(t, app, sessionID, "sthr_live", defaultTestKey), "agent.tool_use")
	putCodeSessionWorkerState(t, app, codeID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	sendSessionEvents(t, app, sessionID, `{"events":[{"type":"user.interrupt"}]}`, defaultTestKey)
	worker, _, err := app.db.GetCodeSession(t.Context(), codeID)
	if err != nil {
		t.Fatal(err)
	}
	sink := sessionsapi.NewHandler(app.cfg, app.db, newCodeSessionService(app, nil, nil), nil, nil, app.vaultSecrets, nil)
	if err := sink.PublishCodeSessionEvents(t.Context(), worker, []json.RawMessage{
		json.RawMessage(`{"type":"session.thread_status_terminated","id":"sevt_child_ended","session_thread_id":"sthr_cancel"}`),
		json.RawMessage(`{"type":"session.status_idle","id":"sevt_parent_idle","stop_reason":{"type":"end_turn"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	child, err := app.db.GetSessionThread(t.Context(), worker.WorkspaceUUID, sessionID, "sthr_cancel")
	if err != nil || child.Status != "terminated" {
		t.Fatalf("child status=%s error=%v", child.Status, err)
	}
	status := sessionEventObjectByType(t, listSessionEvents(t, app, sessionID, "types[]=session.status_idle&order=desc", defaultTestKey), "session.status_idle")
	reason, _ := status["stop_reason"].(map[string]any)
	pendingIDs, _ := reason["event_ids"].([]any)
	if len(pendingIDs) != 1 || pendingIDs[0] != live["primary_event_id"] {
		t.Fatalf("same-batch idle lost live request or reused terminated child: %v", reason)
	}
	for _, id := range []string{tool["id"].(string), tool["primary_event_id"].(string)} {
		resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(id)+`,"result":"allow","session_thread_id":"sthr_cancel"}]}`), defaultTestKey, true)
		assertError(t, resp, http.StatusConflict, "conflict_error")
	}
	if _, err := app.db.UpdateCodeSessionWorkerState(t.Context(), codeID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: worker.CurrentWorkerEpoch, RequiredThreadID: "sthr_cancel", ExternalMetadataSet: true, ExternalMetadata: json.RawMessage(`{}`)}); !errors.Is(err, db.ErrInvalidState) {
		t.Fatalf("late pending persistence accepted terminated child: %v", err)
	}
	postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch,
		`{"type":"control_request","uuid":"late-write","session_thread_id":"sthr_cancel","request_id":"late-request","request":{"subtype":"can_use_tool","tool_name":"Write","tool_use_id":"late-provider","input":{}}}`,
		`{"type":"session.thread_status_idle","uuid":"late-child-idle","session_thread_id":"sthr_cancel","stop_reason":{"type":"requires_action","event_ids":[`+quoteJSON(tool["primary_event_id"].(string))+`]}}`))
	worker, _, err = app.db.GetCodeSession(t.Context(), codeID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := maevents.PendingToolEventIDs(worker.WorkerExternalMetadata, "", "")
	if err != nil || len(pending) != 1 || pending[0] != live["primary_event_id"] {
		t.Fatalf("terminal cleanup lost live child or restored ended child: %v %v", pending, err)
	}
	child, err = app.db.GetSessionThread(t.Context(), worker.WorkspaceUUID, sessionID, "sthr_cancel")
	if err != nil || child.Status != "terminated" {
		t.Fatalf("late idle revived child: %s %v", child.Status, err)
	}
	sendSessionEvents(t, app, sessionID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(live["id"].(string))+`,"result":"allow","session_thread_id":"sthr_live"}]}`, defaultTestKey)
	postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch, `{"type":"result","uuid":"completed-after-approvals","is_error":false}`))
	putCodeSessionWorkerState(t, app, codeID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	status = sessionEventObjectByType(t, listSessionEvents(t, app, sessionID, "types[]=session.status_idle&order=desc", defaultTestKey), "session.status_idle")
	if reason, ok := status["stop_reason"].(map[string]any); ok && reason["type"] == "requires_action" {
		t.Fatalf("completed approvals left waiting state: %v", reason)
	}
}
