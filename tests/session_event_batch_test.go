package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"
)

func TestWorkerHTTPBatchCommitsTogether(t *testing.T) {
	for _, endpoint := range []string{"worker", "legacy"} {
		t.Run(endpoint, func(t *testing.T) {
			app, agent, env := newSessionEventTestApp(t, "worker-batch", `{"model":"claude-opus-4-6","name":"worker-batch","tools":[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"always_ask"}},"configs":[{"name":"bash","permission_policy":{"type":"always_allow"}}]}]}`, `{"name":"worker-batch"}`)
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			session := mustSessionRecord(t, app, response.ID)
			codeSessionID := launchLocalCodeSession(t, app, response.ID)
			epoch := registerCodeSessionWorker(t, app, codeSessionID)
			putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
			childID := "sthr_" + uuid.NewV4().String()
			payloads := []json.RawMessage{
				json.RawMessage(`{"type":"session.thread_created","uuid":"batch-child","session_thread_id":` + quoteJSON(childID) + `,"agent_name":"child"}`),
				json.RawMessage(`{"type":"control_request","uuid":"batch-ask","request_id":"batch-ask","session_thread_id":` + quoteJSON(childID) + `,"request":{"subtype":"can_use_tool","tool_name":"Read","tool_use_id":"batch-ask-tool","input":{"file_path":"/workspace/a"}}}`),
				json.RawMessage(`{"type":"control_request","uuid":"batch-allow-one","request_id":"batch-allow-one","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"batch-allow-one","input":{"command":"pwd-one"}}}`),
				json.RawMessage(`{"type":"control_request","uuid":"batch-allow-two","request_id":"batch-allow-two","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"batch-allow-two","input":{"command":"pwd-two"}}}`),
				json.RawMessage(`{"type":"result","uuid":"batch-idle","stop_reason":"end_turn"}`),
				json.RawMessage(`{"type":"assistant","uuid":"batch-last","message":{"role":"assistant","content":"batch final marker"}}`),
			}
			body, err := json.Marshal(map[string]any{"events": payloads})
			if err != nil {
				t.Fatal(err)
			}
			if endpoint == "worker" {
				events := make([]map[string]json.RawMessage, len(payloads))
				for i, payload := range payloads {
					events[i] = map[string]json.RawMessage{"payload": payload}
				}
				body, err = json.Marshal(map[string]any{"worker_epoch": json.Number(epoch), "events": events})
				if err != nil {
					t.Fatal(err)
				}
			}
			post := func() *http.Response {
				t.Helper()
				if endpoint == "worker" {
					return doCodeSessionWorkerRequest(t, app, codeSessionID, "events", string(body))
				}
				return doSessionEventIngressRequest(t, app, http.MethodPost, codeSessionID, "/events", string(body))
			}
			worker, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			before := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			watermark, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			stream := openSessionEventStream(t, app, ctx, "/v1/sessions/"+response.ID+"/events/stream?beta=true")
			defer stream.Body.Close()
			for _, stage := range []string{"public", "inbound"} {
				var remove func()
				if stage == "public" {
					remove = rejectPublicSessionEventWrites(t, app, session.UUID, "agent.message")
				} else {
					remove = rejectSessionInputCommit(t, app, worker.UUID, "inbound")
				}
				defer remove()
				assertError(t, post(), http.StatusInternalServerError, "api_error")
				remove()
				after, err := getCodeSession(app, t.Context(), codeSessionID)
				if err != nil || after.LastInboundSequenceNum != worker.LastInboundSequenceNum || string(after.WorkerExternalMetadata) != string(worker.WorkerExternalMetadata) {
					t.Fatalf("%s failure committed replies/metadata: %v", stage, err)
				}
				if got := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey); !reflect.DeepEqual(got.Data, before.Data) {
					t.Fatalf("%s failure committed part of history", stage)
				}
				if threads := listSessionThreads(t, app, response.ID, defaultTestKey); len(threads.Data) != 1 || threads.Data[0].Status != "running" || mustSessionRecord(t, app, response.ID).Status != "running" {
					t.Fatalf("%s failure committed thread/status", stage)
				}
				got, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
				if err != nil || !got.Equal(watermark) {
					t.Fatalf("%s failure advanced clock: %v", stage, err)
				}
			}
			resp := post()
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("retry status %d", resp.StatusCode)
			}
			after := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			scanner := bufio.NewScanner(stream.Body)
			for _, raw := range after.Data[len(before.Data):] {
				assertSessionEventJSONEqual(t, assertNextSessionFrameType(t, scanner, sessionEventStringField(t, raw, "type")), raw)
			}
			committed, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || committed.LastInboundSequenceNum != worker.LastInboundSequenceNum+2 || !strings.Contains(string(committed.WorkerExternalMetadata), "batch-ask-tool") {
				t.Fatalf("batch replies or ask did not commit: %v", err)
			}
			queued, err := app.db.ListQueuedCodeSessionInboundEvents(t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			var commands []string
			for _, event := range queued {
				if event.Source == "auto-approve" {
					commands = append(commands, string(event.Payload))
				}
			}
			if len(commands) != 2 || !strings.Contains(commands[0], "pwd-one") || !strings.Contains(commands[1], "pwd-two") {
				t.Fatal("batch automatic replies lost source order")
			}
			resp = post()
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("duplicate status %d", resp.StatusCode)
			}
			if repeated := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey); !reflect.DeepEqual(repeated.Data, after.Data) {
				t.Fatal("batch retry duplicated public events")
			}
			repeated, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || repeated.LastInboundSequenceNum != committed.LastInboundSequenceNum || string(repeated.WorkerExternalMetadata) != string(committed.WorkerExternalMetadata) {
				t.Fatalf("batch retry duplicated replies/metadata: %v", err)
			}
		})
	}
}
