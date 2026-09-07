package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWorkerSystemEventsMatchLiveAndHistory(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "system-events", `{"model":"claude-opus-4-6","name":"system-events"}`, `{"name":"system-events"}`)
	for _, child := range []bool{false, true} {
		name := "worker output"
		if child {
			name = "child transcript"
		}
		t.Run(name, func(t *testing.T) {
			session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			codeSessionID := launchLocalCodeSession(t, app, session.ID)
			epoch := registerCodeSessionWorker(t, app, codeSessionID)
			threadID := ""
			baselineCount := 0
			streamPath := "/v1/sessions/" + session.ID + "/events/stream"
			if child {
				postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[{"payload":{"type":"system","uuid":"child-start","subtype":"task_started","task_id":"system-child","description":"child"}}]}`)
				for _, thread := range listSessionThreads(t, app, session.ID, defaultTestKey).Data {
					if thread.ParentThreadID != nil {
						threadID = thread.ID
					}
				}
				if threadID == "" {
					t.Fatal("missing child thread")
				}
				streamPath = "/v1/sessions/" + session.ID + "/threads/" + threadID + "/stream"
				baselineCount = len(listThreadEvents(t, app, session.ID, threadID, defaultTestKey).Data)
			}
			statusBefore := mustSessionRecord(t, app, session.ID).Status
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			stream := openSessionEventStream(t, app, ctx, streamPath+"?beta=true")
			defer stream.Body.Close()
			inputs := []string{
				`{"type":"system","uuid":"runtime-init","subtype":"init","apiKeySource":"private-runtime-field"}`,
				`{"type":"system","uuid":"runtime-retry","subtype":"api_retry","error_status":500,"error":"server_error","attempt":2,"max_retries":2}`,
				`{"type":"system","uuid":"runtime-compacted","subtype":"compact_boundary","compact_metadata":{"pre_tokens":123}}`,
			}
			if child {
				inputs[1] = `{"type":"system","uuid":"runtime-retry","subtype":"api_error","error":{"status":500,"message":"private-runtime-field"},"retryAttempt":2,"maxRetries":2}`
			}
			for i, raw := range inputs {
				inputs[i] = `{"payload":` + raw + `}`
				if child {
					inputs[i] = `{"agent_id":"system-child","payload":` + raw + `}`
				}
			}
			body := `{"worker_epoch":` + epoch + `,"events":[` + strings.Join(inputs, ",") + `]}`
			post := postCodeSessionWorkerEvents
			if child {
				post = postCodeSessionWorkerInternalEvents
			}
			post(t, app, codeSessionID, body)
			post(t, app, codeSessionID, body)
			var history []json.RawMessage
			if child {
				history = listThreadEvents(t, app, session.ID, threadID, defaultTestKey).Data
				raw := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events?subagents=true")
				assertInternalEventUUIDs(t, raw.Data, []string{"runtime-init", "runtime-retry", "runtime-compacted"})
			} else {
				history = listSessionEvents(t, app, session.ID, "", defaultTestKey).Data
			}
			if len(history) != baselineCount+2 {
				t.Fatalf("public history has %d events, want only retry and compaction: %s", len(history), history)
			}
			history = history[baselineCount:]
			scanner := bufio.NewScanner(stream.Body)
			for i, eventType := range []string{"session.error", "agent.thread_context_compacted"} {
				assertSessionEventJSONEqual(t, assertNextSessionFrameType(t, scanner, eventType), history[i])
			}
			if status := mustSessionRecord(t, app, session.ID).Status; status != statusBefore {
				t.Fatalf("request retry or compaction changed session status: %s -> %s", statusBefore, status)
			}
		})
	}
}
