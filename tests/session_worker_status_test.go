package tests

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionWorkerIdleRetriesAfterPublicWriteFailure(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("idle-retry"))
	codeSession, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	// Simulate worker state being saved before public event publication fails.
	current, found, err := app.db.GetCodeSession(t.Context(), codeSession.ExternalID)
	if err != nil || !found {
		t.Fatalf("worker lookup: %t %v", found, err)
	}
	if _, err := app.db.UpdateCodeSessionWorkerState(t.Context(), codeSession.ExternalID, db.UpdateCodeSessionWorkerStateInput{
		WorkerEpoch: current.CurrentWorkerEpoch, WorkerStatus: new("idle"),
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
	}
	events := listSessionEvents(t, app, codeSession.SessionExternalID, "types[]=session.status_idle", defaultTestKey)
	if len(events.Data) != 1 || retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status != "idle" {
		t.Fatalf("idle publication retry was lost or duplicated: %s", events.Data)
	}
}

func TestSessionWorkerRestartIdleDoesNotEndTurn(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("worker-restart-idle"))
	codeSession, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	epoch = registerCodeSessionWorker(t, app, codeSession.ExternalID)
	// Metadata-only updates must not treat the previous worker's running state
	// as evidence that its replacement has started executing.
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"external_metadata":{"task_summary":null}}`, epoch))
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
	if status := retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("replacement worker initialization ended turn: %s", status)
	}
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
	for _, kind := range []string{"running", "idle"} {
		events := listSessionEvents(t, app, codeSession.SessionExternalID, "types[]=session.status_"+kind, defaultTestKey)
		if len(events.Data) != 1 {
			t.Fatalf("worker restart duplicated %s: %s", kind, events.Data)
		}
	}
}

func TestSessionInputRunningIsAtomic(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("input-running"))
	codeSession, epoch := newPayloadIntegrationSession(t, app)
	path := "/v1/sessions/" + codeSession.SessionExternalID + "/events?beta=true"
	input := `{"type":"user.message","content":[{"type":"text","text":"Hello"}]}`
	// A later invalid thread must roll back the running pair and the first input.
	resp := doSessionRequest(t, app, http.MethodPost, path, strings.NewReader(`{"events":[`+input+`,{"type":"user.message","session_thread_id":"sthr_missing","content":[{"type":"text","text":"Invalid"}]}]}`), defaultTestKey, true)
	assertError(t, resp, http.StatusNotFound, "not_found_error")
	if events := listSessionEvents(t, app, codeSession.SessionExternalID, "", defaultTestKey); len(events.Data) != 0 {
		t.Fatalf("failed batch left events: %s", events.Data)
	}
	if status := retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status; status != "idle" {
		t.Fatalf("failed batch changed status: %s", status)
	}
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			sendSessionEvents(t, app, codeSession.SessionExternalID, `{"events":[`+input+`]}`, defaultTestKey)
		})
	}
	group.Wait()
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, internalPayloadRequest(epoch, `{"type":"session.status_running","uuid":"late-worker-running","id":"sevt_late_worker_running"}`))
	counts := make(map[string]int)
	for _, raw := range listSessionEvents(t, app, codeSession.SessionExternalID, "", defaultTestKey).Data {
		counts[sessionEventStringField(t, raw, "type")]++
	}
	if counts["session.status_running"] != 1 || counts["session.thread_status_running"] != 1 || counts["user.message"] != 2 {
		t.Fatalf("concurrent inputs duplicated running: %v", counts)
	}
}

func TestSessionPublicStatusOrderMatchesLiveHistory(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("public-status-order"))
	codeSession, epoch := newPayloadIntegrationSession(t, app)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+codeSession.SessionExternalID+"/events/stream?beta=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Key", defaultTestKey)
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream: %d", resp.StatusCode)
	}

	for range 2 {
		sent := sendSessionEvents(t, app, codeSession.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"Hello"}]}]}`, defaultTestKey)
		if len(sent.Data) != 1 || sessionEventStringField(t, sent.Data[0], "type") != "user.message" {
			t.Fatalf("send response must contain only submitted events: %s", sent.Data)
		}
		consumePublicInput(t, app, codeSession, epoch, sessionEventStringField(t, sent.Data[0], "id"))
	}
	// Worker initialization reports idle before consuming the accepted inputs.
	for range 2 {
		putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
	}
	if status := retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("accepted input did not activate session: %s", status)
	}
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, internalPayloadRequest(epoch,
		`{"type":"assistant","uuid":"answer","message":{"content":"Hello"}}`,
		`{"type":"result","uuid":"result","is_error":false,"result":"private"}`,
		`{"type":"system.message","uuid":"context","content":[{"type":"text","text":"Public context"}]}`,
	))
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
	want := []string{"session.status_running", "session.thread_status_running", "user.message", "user.message", "agent.message", "system.message", "session.thread_status_idle", "session.status_idle"}
	type publicEvent struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		ThreadID   string `json:"session_thread_id"`
		StopReason struct {
			Type string `json:"type"`
		} `json:"stop_reason"`
	}
	var live []publicEvent
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var event publicEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatal(err)
		}
		if event.ID == "" {
			continue
		}
		live = append(live, event)
		if len(live) == len(want) {
			break
		}
	}
	if len(live) != len(want) {
		t.Fatalf("live events: %+v, scan: %v", live, scanner.Err())
	}
	history := listSessionEvents(t, app, codeSession.SessionExternalID, "order=asc&limit=100", defaultTestKey)
	if len(history.Data) != len(want) {
		t.Fatalf("history: %s", history.Data)
	}
	for i, raw := range history.Data {
		var event publicEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != want[i] || event.ID != live[i].ID || event.Type != live[i].Type || event.StopReason != live[i].StopReason {
			t.Fatalf("event %d: history=%+v live=%+v want=%s", i, event, live[i], want[i])
		}
		if strings.HasPrefix(event.Type, "session.thread_status_") && (event.ThreadID == "" || event.ThreadID != live[i].ThreadID) {
			t.Fatalf("thread identity differs: %+v %+v", event, live[i])
		}
		if strings.Contains(string(raw), "private") {
			t.Fatalf("internal diagnostic leaked: %s", raw)
		}
		if strings.HasSuffix(event.Type, "status_idle") && event.StopReason.Type != "end_turn" {
			t.Fatalf("idle reason: %+v", event)
		}
	}

	sendSessionEvents(t, app, codeSession.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"Next turn"}]}]}`, defaultTestKey)
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
	if status := retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("repeated idle ended newly accepted input: %s", status)
	}
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
	running := listSessionEvents(t, app, codeSession.SessionExternalID, "types[]=session.status_running", defaultTestKey)
	if len(running.Data) != 2 {
		t.Fatalf("expected one running event per turn: %s", running.Data)
	}
	postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, internalPayloadRequest(epoch, `{"type":"control_request","uuid":"approval","request_id":"approval-request","request":{"subtype":"can_use_tool","tool_name":"MysteryTool","tool_use_id":"tool-approval","input":{}}}`))
	toolEvents := listSessionEvents(t, app, codeSession.SessionExternalID, "types[]=agent.tool_use", defaultTestKey)
	if len(toolEvents.Data) != 1 {
		t.Fatalf("approval tool: %s", toolEvents.Data)
	}
	var tool publicEvent
	if err := json.Unmarshal(toolEvents.Data[0], &tool); err != nil {
		t.Fatal(err)
	}
	putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"requires_action"}`, epoch))
	pauses := listSessionEvents(t, app, codeSession.SessionExternalID, "types[]=session.status_idle&types[]=session.thread_status_idle&order=desc&limit=2", defaultTestKey)
	if len(pauses.Data) != 2 {
		t.Fatalf("pause events: %s", pauses.Data)
	}
	for _, raw := range pauses.Data {
		if !strings.Contains(string(raw), `"requires_action"`) || !strings.Contains(string(raw), tool.ID) {
			t.Fatalf("lost approval reason: %s", raw)
		}
	}
}

func consumePublicInput(t *testing.T, app *testApp, session db.CodeSession, epoch, publicID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/code/sessions/"+session.ExternalID+"/worker/events/stream?worker_epoch="+epoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+codeSessionIngressToken(t, app, session.ExternalID))
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("worker stream: %d", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var frame struct {
			EventID string `json:"event_id"`
			Payload struct {
				ID string `json:"id"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			t.Fatal(err)
		}
		if frame.EventID == "" {
			continue
		}
		ack := postCodeSessionWorkerDelivery(t, app, session.ExternalID, `{"worker_epoch":`+quoteJSON(epoch)+`,"updates":[{"event_id":`+quoteJSON(frame.EventID)+`,"status":"processed"}]}`)
		if ack.Applied != 1 {
			t.Fatalf("input was not processed: %+v", ack)
		}
		if frame.Payload.ID == publicID {
			return
		}
	}
	t.Fatalf("input was not delivered: %v", scanner.Err())
}

func TestSessionWorkerIdleHasSingleSource(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("worker-status"))
	codeSession, epoch := newPayloadIntegrationSession(t, app)
	var previousResult string
	for round, order := range []string{"worker first", "result first", "concurrent", "failed result", "worker without result"} {
		t.Run(order, func(t *testing.T) {
			putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"running"}`, epoch))
			if previousResult != "" {
				postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, previousResult)
				if status := retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status; status != "running" {
					t.Fatalf("late result ended the next turn: %s", status)
				}
			}
			result := fmt.Sprintf(`{"worker_epoch":%s,"events":[{"payload":{"type":"result","uuid":"result-%d","is_error":%t,"duration_api_ms":4324,"result":"execution details"}}]}`, epoch, round, order == "failed result")
			publishResult := func() { postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, result) }
			publishIdle := func() {
				putCodeSessionWorkerState(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%s,"worker_status":"idle"}`, epoch))
			}
			switch order {
			case "worker first":
				publishIdle()
				publishResult()
			case "concurrent":
				var group sync.WaitGroup
				group.Go(publishIdle)
				group.Go(publishResult)
				group.Wait()
			case "worker without result":
				publishIdle()
			default:
				publishResult()
				if status := retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status; status != "running" {
					t.Fatalf("result changed worker state to %s", status)
				}
				publishIdle()
			}
			// Retries must not emit another status transition.
			publishIdle()
			if order != "worker without result" {
				publishResult()
				previousResult = result
			}
			events := listSessionEvents(t, app, codeSession.SessionExternalID, "types[]=session.status_idle&limit=100", defaultTestKey)
			if len(events.Data) != round+1 {
				t.Fatalf("idle events=%d, want %d (one per round)", len(events.Data), round+1)
			}
			if status := retrieveSession(t, app, codeSession.SessionExternalID, defaultTestKey).Status; status != "idle" {
				t.Fatalf("worker idle projected status=%s", status)
			}
		})
	}
}
