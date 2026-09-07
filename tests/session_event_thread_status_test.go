package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWorkerThreadStatusControlsSessionEvents(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "thread-status-events", `{"model":"claude-opus-4-6","name":"thread-status-events"}`, `{"name":"thread-status-events"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	codeSessionID := launchLocalCodeSession(t, app, response.ID)
	epoch := registerCodeSessionWorker(t, app, codeSessionID)
	putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[{"payload":{"type":"system","uuid":"thread-start","subtype":"task_started","task_id":"state-child","description":"child"}}]}`)
	threads := listSessionThreads(t, app, response.ID, defaultTestKey)
	var primaryID, childID string
	for _, thread := range threads.Data {
		if thread.ParentThreadID == nil {
			primaryID = thread.ID
		} else {
			childID = thread.ID
		}
	}
	if primaryID == "" || childID == "" {
		t.Fatal("missing primary or child")
	}
	before := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	stream := openSessionEventStream(t, app, ctx, "/v1/sessions/"+response.ID+"/events/stream?beta=true")
	defer stream.Body.Close()
	// First primary idle must not idle a Session whose child is still running.
	putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	primary := retrieveSessionThread(t, app, response.ID, primaryID, defaultTestKey)
	if primary.Status != "idle" || mustSessionRecord(t, app, response.ID).Status != "running" {
		t.Fatal("primary idle overrode running child aggregate")
	}
	mid := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
	if len(mid.Data) != len(before.Data)+1 || !strings.Contains(string(mid.Data[len(before.Data)]), "session.thread_status_idle") {
		t.Fatal("first primary idle did not publish exactly its thread fact")
	}
	childEventID := "sevt_" + uuid.NewV4().String()
	childIdle := `{"worker_epoch":` + epoch + `,"events":[{"payload":{"type":"session.thread_status_idle","uuid":"child-idle","id":` + quoteJSON(childEventID) + `,"session_thread_id":` + quoteJSON(childID) + `}}]}`
	removeFailure := rejectPublicSessionEventWrites(t, app, session.UUID, "session.status_idle")
	defer removeFailure()
	assertError(t, doCodeSessionWorkerRequest(t, app, codeSessionID, "events", childIdle), http.StatusInternalServerError, "api_error")
	if child := retrieveSessionThread(t, app, response.ID, childID, defaultTestKey); child.Status != "running" {
		t.Fatal("failed aggregate write committed child idle")
	}
	if failed := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey); !reflect.DeepEqual(failed.Data, mid.Data) {
		t.Fatal("failed aggregate write committed part of its history")
	}
	removeFailure()
	postCodeSessionWorkerEvents(t, app, codeSessionID, childIdle)
	after := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
	if len(after.Data) != len(before.Data)+3 || mustSessionRecord(t, app, response.ID).Status != "idle" {
		t.Fatal("last child idle did not publish the aggregate transition")
	}
	scanner := bufio.NewScanner(stream.Body)
	for i, eventType := range []string{"session.thread_status_idle", "session.thread_status_idle", "session.status_idle"} {
		live := assertNextSessionFrameType(t, scanner, eventType)
		assertSessionEventJSONEqual(t, live, after.Data[len(before.Data)+i])
		if i == 0 && sessionEventStringField(t, live, "agent_name") != "thread-status-events" {
			t.Fatalf("primary thread status lost agent_name: %s", live)
		}
	}
	// Retry an old child idle after that child resumes. Neither its own state
	// nor a new aggregate event may be generated from the old source again.
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[{"payload":{"type":"session.thread_status_running","uuid":"child-resume","session_thread_id":`+quoteJSON(childID)+`}}]}`)
	if _, err := app.pool.Exec(t.Context(), `UPDATE session_threads SET agent_snapshot = '{"name":"renamed-child"}' WHERE workspace_uuid = $1 AND session_uuid = $2 AND external_id = $3`, session.WorkspaceUUID, session.UUID, childID); err != nil {
		t.Fatal(err)
	}
	resumed := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
	postCodeSessionWorkerEvents(t, app, codeSessionID, childIdle)
	if repeated := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey); !reflect.DeepEqual(repeated.Data, resumed.Data) {
		t.Fatal("old thread fact generated a new aggregate event on retry")
	}
	if child := retrieveSessionThread(t, app, response.ID, childID, defaultTestKey); child.Status != "running" || mustSessionRecord(t, app, response.ID).Status != "running" {
		t.Fatal("old thread fact overwrote resumed state")
	}
	// A pre-upgrade row may lack agent_name entirely. Retrying must preserve
	// absence instead of treating it as an explicit empty string.
	if _, err := app.pool.Exec(t.Context(), `UPDATE session_events SET payload = payload - 'agent_name' WHERE workspace_uuid = $1 AND session_uuid = $2 AND external_id = $3`, session.WorkspaceUUID, session.UUID, childEventID); err != nil {
		t.Fatal(err)
	}
	legacy := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
	postCodeSessionWorkerEvents(t, app, codeSessionID, childIdle)
	if repeated := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey); !reflect.DeepEqual(repeated.Data, legacy.Data) {
		t.Fatal("legacy thread fact changed on retry")
	}
}

func TestSessionIdlePreservesPendingChildActions(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "child-actions", `{"model":"claude-opus-4-6","name":"child-actions","tools":[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"always_ask"}}}]}`, `{"name":"child-actions"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, response.ID)
	epoch := registerCodeSessionWorker(t, app, codeSessionID)
	putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	ask := func(childID, requestID string) string {
		t.Helper()
		postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[{"payload":{"type":"session.thread_status_running","uuid":`+quoteJSON("start-"+requestID)+`,"session_thread_id":`+quoteJSON(childID)+`,"agent_name":"child"}},{"payload":{"type":"control_request","uuid":`+quoteJSON(requestID)+`,"request_id":`+quoteJSON(requestID)+`,"session_thread_id":`+quoteJSON(childID)+`,"request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":`+quoteJSON("tool-"+requestID)+`,"input":{"command":"pwd"}}}}]}`)
		page := listSessionEvents(t, app, response.ID, "types[]=agent.tool_use&limit=100", defaultTestKey)
		var event struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(page.Data[len(page.Data)-1], &event); err != nil {
			t.Fatal(err)
		}
		return event.ID
	}
	assertIdle := func(expectedIDs ...string) {
		t.Helper()
		page := listSessionEvents(t, app, response.ID, "types[]=session.status_idle&limit=100", defaultTestKey)
		if len(page.Data) == 0 {
			t.Fatal("missing aggregate idle")
		}
		var event struct {
			StopReason struct {
				Type     string   `json:"type"`
				EventIDs []string `json:"event_ids"`
			} `json:"stop_reason"`
		}
		if err := json.Unmarshal(page.Data[len(page.Data)-1], &event); err != nil {
			t.Fatal(err)
		}
		slices.Sort(expectedIDs)
		wantType := "end_turn"
		if len(expectedIDs) > 0 {
			wantType = "requires_action"
		}
		if event.StopReason.Type != wantType || !slices.Equal(event.StopReason.EventIDs, expectedIDs) {
			t.Fatalf("aggregate reason = %+v, want %s %v", event.StopReason, wantType, expectedIDs)
		}
	}
	first := ask("sthr_"+uuid.NewV4().String(), "first-child-ask")
	if got := mustSessionRecord(t, app, response.ID).Status; got != "running" {
		t.Fatalf("child ask stopped running primary: %s", got)
	}
	putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	assertIdle(first)
	second := ask("sthr_"+uuid.NewV4().String(), "second-child-ask")
	assertIdle(first, second)
	// Move the first request to the legacy single-key representation. A normal
	// confirmation must clear that actual record, without clearing the second.
	worker, err := getCodeSession(app, t.Context(), codeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(worker.WorkerExternalMetadata, &metadata); err != nil {
		t.Fatal(err)
	}
	key := "managed_agent_tool_permission_request:" + first
	patch, err := json.Marshal(map[string]any{"worker_epoch": json.Number(epoch), "external_metadata": map[string]any{key: nil, "managed_agent_tool_permission_request": metadata[key]}})
	if err != nil {
		t.Fatal(err)
	}
	putCodeSessionWorkerState(t, app, codeSessionID, string(patch))
	for i, id := range []string{first, second} {
		sendSessionEvents(t, app, response.ID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(id)+`,"result":"allow"}]}`, defaultTestKey)
		putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
		putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
		if i == 0 {
			assertIdle(second)
		} else {
			assertIdle()
		}
	}
	// Another runtime may be newer, but its pending request does not belong to
	// this source worker's idle event.
	session := mustSessionRecord(t, app, response.ID)
	other, err := app.db.CreateCodeSession(t.Context(), db.CreateCodeSessionInput{
		ExternalID: "cse_" + uuid.NewV4().String(), OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID,
		SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID,
		WorkDir: "/workspace", PermissionMode: "default", Model: "claude-opus-4-6", Status: "active", InitialWorkerEpoch: 1,
		Metadata: json.RawMessage(`{}`), CreatedAt: time.Now().UTC().Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = app.db.WithManagedAgentEventTx(t.Context(), func(tx db.ManagedAgentEventTx) error {
		locked, err := tx.LockSessionForEvents(t.Context(), session.WorkspaceUUID, session.ExternalID)
		if err != nil {
			return err
		}
		if _, found, err := tx.GetSessionCodeSession(t.Context(), db.Session{WorkspaceUUID: uuid.NewV4().String(), UUID: locked.UUID}, codeSessionID); err != nil || found {
			t.Fatalf("source lookup crossed workspace: found=%v error=%v", found, err)
		}
		return tx.MergeWorkerMetadata(t.Context(), other, json.RawMessage(`{"managed_agent_tool_permission_request":{"public_event_id":"sevt_other","request_id":"other","provider_tool_use_id":"other"}}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	assertIdle()
}
