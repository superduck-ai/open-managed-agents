//go:build integration || e2e || e2b_integration

package tests

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSessionInterruptRedirectContract(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("interrupt-redirect"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	path := "/v1/sessions/" + worker.SessionExternalID + "/events"
	for _, bad := range []string{`{"type":"user.interrupt","session_thread_id":false}`, `{"type":"user.interrupt","session_thread_id":" "}`, `{"type":"user.interrupt","_interrupt_input_id":"forged"}`, `{"type":"user.interrupt","cancel_input_id":"forged"}`} {
		assertError(t, doSessionRequest(t, app, http.MethodPost, path+"?beta=true", strings.NewReader(`{"events":[`+bad+`]}`), defaultTestKey, true), http.StatusBadRequest, "invalid_request_error")
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config,worker_transport}','"claim"') WHERE external_id=$1`, worker.ExternalID); err != nil {
		t.Fatal(err)
	}
	active := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"delegate"}]}]}`, defaultTestKey)
	activeID := sessionEventStringField(t, active.Data[0], "id")
	consumePublicInput(t, app, worker, epoch, activeID)
	sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"queued before interrupt"}]}]}`, defaultTestKey)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+path+"/stream?beta=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamReq.Header.Set("X-Api-Key", defaultTestKey)
	streamReq.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	stream, err := app.client.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	send := func() []map[string]any {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+path+"?beta=true", strings.NewReader(`{"events":[{"type":"user.interrupt"},{"type":"user.message","content":[{"type":"text","text":"redirect"}]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header = streamReq.Header.Clone()
		req.Header.Set("Idempotency-Key", "redirect")
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.UnmarshalRead(resp.Body, &body); err != nil || resp.StatusCode != http.StatusOK || len(body.Data) != 2 {
			t.Fatalf("send=%d events=%v err=%v", resp.StatusCode, body.Data, err)
		}
		return body.Data
	}
	queued := send()
	interruptID := queued[0]["id"].(string)
	if len(queued[0]) != 3 || queued[0]["type"] != "user.interrupt" || queued[0]["processed_at"] != nil {
		t.Fatalf("queued interrupt: %v", queued[0])
	}
	if queued[1]["processed_at"] != nil {
		t.Fatalf("redirect already processed: %v", queued[1])
	}
	replay := send()
	if replay[0]["id"] != interruptID || replay[1]["id"] != queued[1]["id"] {
		t.Fatalf("duplicate redirect: %v", replay)
	}
	history := func() map[string]any {
		t.Helper()
		events := listSessionEvents(t, app, worker.SessionExternalID, "types[]=user.interrupt", defaultTestKey)
		if len(events.Data) != 1 {
			t.Fatalf("interrupt count: %v", events.Data)
		}
		return sessionEventObjectByType(t, events, "user.interrupt")
	}
	if got := history(); len(got) != 3 || got["processed_at"] != nil {
		t.Fatal(got)
	}
	inputs, err := listQueuedCodeSessionInboundEvents(app, worker.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range inputs {
		var input struct {
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
				InputID string `json:"input_id"`
			} `json:"request"`
		}
		if err := json.Unmarshal(e.Payload, &input); err != nil {
			t.Fatal(err)
		}
		if input.RequestID == interruptID {
			found = input.Request.Subtype == "interrupt" && input.Request.InputID == activeID
		}
	}
	if !found {
		t.Fatal("interrupt was not bound to the running input")
	}
	update := func(status string) {
		postCodeSessionWorkerDelivery(t, app, worker.ExternalID, `{"worker_epoch":`+quoteJSON(epoch)+`,"updates":[{"event_id":`+quoteJSON(interruptID)+`,"status":`+quoteJSON(status)+`}]}`)
	}
	update("processing")
	if history()["processed_at"] != nil {
		t.Fatal("progress acknowledged interrupt")
	}
	update("processed")
	applied := history()
	if len(applied) != 3 || applied["id"] != interruptID || applied["processed_at"] == nil {
		t.Fatalf("applied interrupt: %v", applied)
	}
	update("processed")
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"result","uuid":`+quoteJSON(activeID+"_result")+`,"is_error":false}`))
	scanner := bufio.NewScanner(stream.Body)
	acknowledged := false
	for scanner.Scan() {
		raw, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		switch event["type"] {
		case "user.interrupt":
			if acknowledged || len(event) != 3 || event["id"] != interruptID || event["processed_at"] != applied["processed_at"] {
				t.Fatalf("premature or duplicate stream receipt: %v", event)
			}
			acknowledged = true
		case "session.status_idle":
			if !acknowledged || event["stop_reason"].(map[string]any)["type"] != "end_turn" {
				t.Fatalf("idle before interrupt or wrong reason: %+v", event)
			}
			if got := send(); got[0]["processed_at"] != applied["processed_at"] {
				t.Fatalf("receipt replay changed: %v", got)
			}
			return
		}
	}
	t.Fatalf("missing interrupt/idle: %v", scanner.Err())
}

func TestLegacyInterruptOnlySettlesAtCurrentCompletion(t *testing.T) {
	for _, state := range []string{"running", "awaiting approval"} {
		t.Run(state, func(t *testing.T) {
			checkLegacyInterruptAtCompletion(t, state == "awaiting approval")
		})
	}
}

func checkLegacyInterruptAtCompletion(t *testing.T, awaitingApproval bool) {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("legacy-interrupt"))
	worker, epoch := newPayloadIntegrationSessionWithAgent(t, app, bashAskAgentBody)
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET metadata=metadata #- '{config,worker_transport}' WHERE external_id=$1`, worker.ExternalID); err != nil {
		t.Fatal(err)
	}
	active := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"run bash"}]}]}`, defaultTestKey)
	inputID := sessionEventStringField(t, active.Data[0], "id")
	consumePublicInput(t, app, worker, epoch, inputID)
	if awaitingApproval {
		postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"control_request","uuid":"approval","request_id":"approval-request","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"approval","input":{"command":"date"}}}`))
	}
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.interrupt"},{"type":"user.message","content":[{"type":"text","text":"redirect"}]}]}`, defaultTestKey)
	stopID := sessionEventStringField(t, sent.Data[0], "id")
	child := "sthr_child_" + worker.SessionExternalID
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.thread_status_running","uuid":"child-running","session_thread_id":`+quoteJSON(child)+`}`))
	if sessionInputProcessedAt(t, sent.Data[0]) != "" {
		t.Fatal("interrupt applied before approval was cancelled")
	}
	history := func() map[string]any {
		return sessionEventObjectByType(t, listSessionEvents(t, app, worker.SessionExternalID, "types[]=user.interrupt", defaultTestKey), "user.interrupt")
	}
	for _, stale := range []string{`{"type":"result","uuid":"sevt_older_result","is_error":false}`, `{"type":"result","uuid":"delayed","created_at":"2000-01-01T00:00:00Z","is_error":false}`} {
		postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, stale))
		if got := history(); got["processed_at"] != nil {
			t.Fatalf("stale completion settled a newer interrupt: %v", got)
		}
	}
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"result","uuid":`+quoteJSON(inputID+"_result")+`,"is_error":false}`))
	if got := history(); got["processed_at"] != nil {
		t.Fatalf("primary completion applied global interrupt while child was running: %v", got)
	}
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.thread_status_idle","uuid":"child-idle","session_thread_id":`+quoteJSON(child)+`,"stop_reason":{"type":"end_turn"}}`))
	if got := history(); got["id"] != stopID || got["processed_at"] == nil {
		t.Fatalf("missing applied interrupt: %v", got)
	}
	idle := sessionEventObjectByType(t, listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.status_idle&order=desc", defaultTestKey), "session.status_idle")
	if idle["stop_reason"].(map[string]any)["type"] != "end_turn" {
		t.Fatal(idle)
	}
	redirectID := sessionEventStringField(t, sent.Data[1], "id")
	consumePublicInput(t, app, worker, epoch, redirectID)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	primary, found, err := app.db.GetPrimarySessionThreadExternalID(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatal(err)
	}
	directed := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.interrupt","session_thread_id":`+quoteJSON(primary)+`}]}`, defaultTestKey)
	directedID := sessionEventStringField(t, directed.Data[0], "id")
	if sessionEventStringField(t, directed.Data[0], "session_thread_id") != primary || sessionInputProcessedAt(t, directed.Data[0]) != "" {
		t.Fatal(string(directed.Data[0]))
	}
	consumePublicInput(t, app, worker, epoch, directedID)
	latest := sessionEventObjectByType(t, listSessionEvents(t, app, worker.SessionExternalID, "types[]=user.interrupt&order=desc", defaultTestKey), "user.interrupt")
	if latest["id"] != directedID || latest["session_thread_id"] != primary || latest["processed_at"] == nil {
		t.Fatal(latest)
	}
}
