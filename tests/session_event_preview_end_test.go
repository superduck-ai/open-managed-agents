package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionPreviewEndAndTerminalScopes(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "preview-end", `{"model":"claude-opus-4-6","name":"preview-end"}`, `{"name":"preview-end"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	workerID := launchLocalCodeSession(t, app, response.ID)
	epoch := registerCodeSessionWorker(t, app, workerID)
	putCodeSessionWorkerState(t, app, workerID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	// Reuse the worker's existing credential when simulating in-flight output
	// after termination; a terminated Session cannot issue a new credential.
	token := codeSessionIngressToken(t, app, workerID)
	childID := "sthr_" + uuid.NewV4().String()
	post := func(raw string) {
		t.Helper()
		result := doCodeSessionWorkerRequestWithToken(t, app, http.MethodPost, workerID, "events", `{"worker_epoch":`+epoch+`,"events":[{"payload":`+raw+`}]}`, token)
		defer result.Body.Close()
		if result.StatusCode != http.StatusOK {
			t.Fatalf("post worker events status = %d: %s", result.StatusCode, readAll(t, result.Body))
		}
	}
	post(`{"uuid":"` + uuid.NewV4().String() + `","type":"session.thread_created","session_thread_id":` + quoteJSON(childID) + `,"agent_name":"child"}`)
	var primaryID string
	for _, thread := range listSessionThreads(t, app, response.ID, defaultTestKey).Data {
		if thread.ParentThreadID == nil {
			primaryID = thread.ID
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	open := func(threadID string) (*http.Response, *bufio.Scanner) {
		t.Helper()
		path := "/v1/sessions/" + response.ID + "/events/stream"
		if threadID != "" {
			path = "/v1/sessions/" + response.ID + "/threads/" + threadID + "/stream"
		}
		stream := openSessionEventStream(t, app, ctx, path+"?beta=true&event_deltas=agent.message")
		return stream, bufio.NewScanner(stream.Body)
	}
	primaryStream, primaryScanner := open("")
	defer primaryStream.Body.Close()
	childStream, childScanner := open(childID)
	defer childStream.Body.Close()
	requests := map[string]string{primaryID: uuid.NewV4().String(), childID: uuid.NewV4().String()}
	for _, scope := range []struct {
		id      string
		scanner *bufio.Scanner
	}{{primaryID, primaryScanner}, {childID, childScanner}} {
		post(`{"uuid":"` + uuid.NewV4().String() + `","id":` + quoteJSON(requests[scope.id]) + `,"type":"span.model_request_start","owner_session_thread_id":` + quoteJSON(scope.id) + `,"model":"test"}`)
		assertNextSessionFrameType(t, scope.scanner, "span.model_request_start")
	}
	start := func(threadID, id string) {
		t.Helper()
		post(`{"uuid":"` + uuid.NewV4().String() + `","type":"event_start","owner_session_thread_id":` + quoteJSON(threadID) + `,"model_request_start_id":` + quoteJSON(requests[threadID]) + `,"event":{"id":` + quoteJSON(id) + `,"type":"agent.message"}}`)
	}
	delta := func(threadID, id, text string) {
		t.Helper()
		post(`{"uuid":"` + uuid.NewV4().String() + `","type":"event_delta","owner_session_thread_id":` + quoteJSON(threadID) + `,"model_request_start_id":` + quoteJSON(requests[threadID]) + `,"event_id":` + quoteJSON(id) + `,"delta":{"type":"content_delta","index":0,"content":{"type":"text","text":` + quoteJSON(text) + `}}}`)
	}
	primaryPreview, childPreview := uuid.NewV4().String(), uuid.NewV4().String()
	start(primaryID, primaryPreview)
	start(childID, childPreview)
	assertNextSessionFrameType(t, primaryScanner, "event_start")
	assertNextSessionFrameType(t, childScanner, "event_start")
	delta(primaryID, primaryPreview, "primary continues")
	delta(childID, childPreview, "child unfinished")
	assertNextSessionFrameType(t, primaryScanner, "event_delta")
	assertNextSessionFrameType(t, childScanner, "event_delta")

	// End belongs to the child connection; no final message is fabricated.
	post(`{"uuid":"` + uuid.NewV4().String() + `","type":"span.model_request_end","owner_session_thread_id":` + quoteJSON(childID) + `,"model_request_start_id":` + quoteJSON(requests[childID]) + `,"is_error":true,"model_usage":{}}`)
	assertNextSessionFrameType(t, childScanner, "span.model_request_end")
	start(childID, childPreview)
	delta(childID, childPreview, "must not reopen after end")
	requests[childID] = uuid.NewV4().String()
	post(`{"uuid":"` + uuid.NewV4().String() + `","id":` + quoteJSON(requests[childID]) + `,"type":"span.model_request_start","owner_session_thread_id":` + quoteJSON(childID) + `,"model":"test"}`)
	assertNextSessionFrameType(t, childScanner, "span.model_request_start")
	nextChildPreview := uuid.NewV4().String()
	start(childID, nextChildPreview)
	frame := assertNextSessionFrameType(t, childScanner, "event_start")
	var started struct {
		Event struct {
			ID string `json:"id"`
		} `json:"event"`
	}
	if err := json.Unmarshal(frame, &started); err != nil || started.Event.ID != nextChildPreview {
		t.Fatalf("closed preview reopened: %s", frame)
	}

	// Persist without notifying: the child stream must observe this primary-only
	// control through the ordered log, without leaking it into child history/SSE.
	controlID := uuid.NewV4().String()
	_, err := app.db.AppendSessionEvents(t.Context(), session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{{
		UUID: uuid.NewV4().String(), ExternalID: controlID, EventType: "session.thread_status_terminated",
		Payload:     json.RawMessage(`{"id":` + quoteJSON(controlID) + `,"type":"session.thread_status_terminated","session_thread_id":` + quoteJSON(childID) + `}`),
		StateChange: &db.SessionEventStateChange{ThreadExternalID: childID, Status: "terminated", AggregateSession: true},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertNextSessionFrameType(t, primaryScanner, "session.thread_status_terminated")
	delta(primaryID, primaryPreview, "primary still open after child termination")
	assertNextSessionFrameType(t, primaryScanner, "event_delta")
	start(childID, uuid.NewV4().String()) // forces catch-up even if the control notification was lost
	delta(childID, nextChildPreview, "must not survive terminal control")

	// A new live-only connection also checks already-committed terminal state.
	newChildStream, newChildScanner := open(childID)
	defer newChildStream.Body.Close()
	start(childID, uuid.NewV4().String())
	markerID := uuid.NewV4().String()
	post(`{"uuid":` + quoteJSON(markerID) + `,"id":` + quoteJSON(markerID) + `,"type":"agent.message","owner_session_thread_id":` + quoteJSON(childID) + `,"content":[{"type":"text","text":"late complete child fact"}]}`)
	childFinal := assertNextSessionFrameType(t, childScanner, "agent.message")
	assertSessionEventJSONEqual(t, assertNextSessionFrameType(t, newChildScanner, "agent.message"), childFinal)
	childHistory := listThreadEvents(t, app, response.ID, childID, defaultTestKey)
	if len(childHistory.Data) != 4 {
		t.Fatalf("child history contains controls or previews: %s", childHistory.Data)
	}
	assertSessionEventJSONEqual(t, childHistory.Data[len(childHistory.Data)-1], childFinal)

	post(`{"uuid":"` + uuid.NewV4().String() + `","type":"session.status_terminated"}`)
	assertNextSessionFrameType(t, primaryScanner, "session.status_terminated")
	start(primaryID, primaryPreview)
	start(primaryID, uuid.NewV4().String())
	delta(primaryID, primaryPreview, "must not survive Session termination")
	finalID := uuid.NewV4().String()
	post(`{"uuid":` + quoteJSON(finalID) + `,"id":` + quoteJSON(finalID) + `,"type":"agent.message","content":[{"type":"text","text":"late complete primary fact"}]}`)
	primaryFinal := assertNextSessionFrameType(t, primaryScanner, "agent.message")
	history := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
	assertSessionEventJSONEqual(t, history.Data[len(history.Data)-1], primaryFinal)
	if eventPageContains(history, "must not") || eventPageContains(history, "child unfinished") {
		t.Fatal("preview was persisted")
	}
}
