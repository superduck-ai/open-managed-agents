package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSessionWorkerFailedTurnStopReason(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("failed-turn"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	state := func(status string) {
		putCodeSessionWorkerState(t, app, worker.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":%q}`, epoch, status))
	}
	state("running")
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"assistant","uuid":"api-error-message","is_api_error_message":true,"message":{"id":"synthetic-error","content":[{"type":"text","text":"private provider failure"}]}}`,
		`{"type":"result","uuid":"failed-result","is_error":true,"subtype":"error_during_execution"}`,
	))
	if messages := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.message", defaultTestKey); len(messages.Data) != 0 {
		t.Fatalf("API error exposed as assistant reply: %s", messages.Data)
	}
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "idle" {
		t.Fatalf("exhausted result did not end execution: %s", status)
	}
	state("idle")
	state("idle")
	assertWorkerIdleReasons(t, app, worker.SessionExternalID, "retries_exhausted")
	response := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+worker.SessionExternalID+"/events?beta=true",
		strings.NewReader(`{"events":[{"type":"user.message","content":[{"type":"text","text":"Retry"}]}]}`), defaultTestKey, true)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("retry input status=%d: %s", response.StatusCode, readAll(t, response.Body))
	}
	state("running")
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"assistant","uuid":"normal-message","message":{"id":"normal-reply","content":[{"type":"text","text":"Recovered"}]}}`,
		`{"type":"result","uuid":"successful-result","is_error":false}`,
	))
	state("idle")
	state("idle")
	assertWorkerIdleReasons(t, app, worker.SessionExternalID, "retries_exhausted", "end_turn")
	if messages := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.message", defaultTestKey); len(messages.Data) != 1 {
		t.Fatalf("successful reply missing or duplicated: %s", messages.Data)
	}
}

func assertWorkerIdleReasons(t *testing.T, app *testApp, sessionID string, expected ...string) {
	t.Helper()
	events := listSessionEvents(t, app, sessionID, "types[]=session.status_idle&order=asc", defaultTestKey)
	if len(events.Data) != len(expected) {
		t.Fatalf("idle events=%s, want reasons=%v", events.Data, expected)
	}
	for index, raw := range events.Data {
		var event struct {
			StopReason struct {
				Type string `json:"type"`
			} `json:"stop_reason"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.StopReason.Type != expected[index] {
			t.Fatalf("idle reason=%s, want %s", event.StopReason.Type, expected[index])
		}
	}
}

func TestSessionWorkerFailedTurnIgnoresChildReply(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("failed-primary-turn"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, worker.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"system","uuid":"child-started","subtype":"task_started","task_id":"child-task","tool_use_id":"child-tool","description":"Child"}`,
		`{"type":"result","uuid":"primary-failed","is_error":true}`,
		`{"type":"assistant","uuid":"child-answer","parent_tool_use_id":"child-tool","message":{"id":"child-reply","content":[{"type":"text","text":"Child completed"}]}}`,
		`{"type":"system","uuid":"child-completed","subtype":"task_notification","task_id":"child-task","tool_use_id":"child-tool","status":"completed"}`,
	))
	putCodeSessionWorkerState(t, app, worker.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
	assertWorkerIdleReasons(t, app, worker.SessionExternalID, "end_turn", "retries_exhausted")
}
