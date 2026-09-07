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

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWorkerSessionErrorContractAndSuccessfulResult(t *testing.T) {
	for _, endpoint := range []string{"worker", "legacy", "persistence-post", "persistence-put"} {
		t.Run(endpoint, func(t *testing.T) {
			app, agent, env := newSessionEventTestApp(t, "event-contract", `{"model":"claude-opus-4-6","name":"event-contract"}`, `{"name":"event-contract"}`)
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			workerID := launchLocalCodeSession(t, app, response.ID)
			epoch := registerCodeSessionWorker(t, app, workerID)
			putCodeSessionWorkerState(t, app, workerID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
			session := mustSessionRecord(t, app, response.ID)
			post := func(payloads ...string) *http.Response {
				t.Helper()
				if endpoint == "worker" {
					events := make([]string, len(payloads))
					for i, raw := range payloads {
						events[i] = `{"payload":` + raw + `}`
					}
					return doCodeSessionWorkerRequest(t, app, workerID, "events", `{"worker_epoch":`+epoch+`,"events":[`+strings.Join(events, ",")+`]}`)
				}
				suffix, method := "/events", http.MethodPost
				if strings.HasPrefix(endpoint, "persistence-") {
					suffix = ""
					if endpoint == "persistence-put" {
						method = http.MethodPut
					}
				}
				return doSessionEventIngressRequest(t, app, method, workerID, suffix, `{"events":[`+strings.Join(payloads, ",")+`]}`)
			}
			postOK := func(payloads ...string) {
				t.Helper()
				result := post(payloads...)
				defer result.Body.Close()
				if result.StatusCode != http.StatusOK {
					t.Fatalf("post status = %d: %s", result.StatusCode, readAll(t, result.Body))
				}
			}
			assertInvalid := func(result *http.Response) {
				t.Helper()
				defer result.Body.Close()
				var body struct {
					Error struct {
						Type    string `json:"type"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := json.NewDecoder(result.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if result.StatusCode != http.StatusBadRequest || body.Error.Type != "invalid_request_error" || body.Error.Message != "Invalid worker event payload" {
					t.Fatalf("unexpected protocol response: status=%d body=%+v", result.StatusCode, body)
				}
			}
			before := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			clockBefore, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			resultID := uuid.NewV4().String()
			success := `{"uuid":` + quoteJSON(resultID) + `,"type":"result","subtype":"success","is_error":false,"stop_reason":"stop_sequence","result":"done"}`
			childID := "sthr_" + uuid.NewV4().String()
			prefix := `{"uuid":` + quoteJSON(uuid.NewV4().String()) + `,"type":"session.thread_created","session_thread_id":` + quoteJSON(childID) + `,"agent_name":"rolled-back"}`
			for _, invalid := range []string{
				`"private-runtime-error"`,
				`{"type":"unknown_error","message":"private-runtime-error","retry_status":"terminal"}`,
				`{"type":"mcp_connection_failed_error","message":"private-runtime-error","retry_status":{"type":"retrying"}}`,
			} {
				bad := `{"uuid":` + quoteJSON(uuid.NewV4().String()) + `,"type":"session.error","owner_session_thread_id":` + quoteJSON(childID) + `,"error":` + invalid + `}`
				assertInvalid(post(success, prefix, bad))
			}
			// JSON numeric overflow errors include the input number; the HTTP
			// boundary must return its stable message instead of the decoder cause.
			assertInvalid(post(success, prefix, `{"uuid":`+quoteJSON(uuid.NewV4().String())+`,"type":"result","duration_ms":1e1000}`))
			if after := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey); !reflect.DeepEqual(after.Data, before.Data) {
				t.Fatal("invalid error committed part of its batch")
			}
			clockAfter, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !clockAfter.Equal(clockBefore) {
				t.Fatalf("invalid error advanced the clock: %v", err)
			}
			if current := mustSessionRecord(t, app, response.ID); current.Status != session.Status {
				t.Fatal("invalid error committed the preceding result's idle state")
			}
			if len(listSessionThreads(t, app, response.ID, defaultTestKey).Data) != 1 {
				t.Fatal("invalid error committed a new thread")
			}

			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			stream := openSessionEventStream(t, app, ctx, "/v1/sessions/"+response.ID+"/events/stream?beta=true")
			defer stream.Body.Close()
			errorID := uuid.NewV4().String()
			canonical := `{"uuid":` + quoteJSON(errorID) + `,"id":` + quoteJSON(errorID) + `,"type":"session.error","error":{"type":"credential_host_unreachable_error","message":"","credential_id":"cred","vault_id":"vault","retry_status":{"type":"retrying"},"future_counter":9007199254740993}}`
			postOK(canonical)
			postOK(canonical)
			assertError(t, post(strings.Replace(canonical, `"type":"retrying"`, `"type":"terminal"`, 1)), http.StatusConflict, "conflict_error")
			if current := mustSessionRecord(t, app, response.ID); current.Status != session.Status {
				t.Fatal("error validation inferred a Session transition")
			}
			postOK(success)
			postOK(success)
			// Older stored extension codes remain readable; validation only governs
			// new canonical worker writes and never rewrites public history.
			futureID := uuid.NewV4().String()
			_, err = app.db.AppendSessionEvents(t.Context(), session.WorkspaceUUID, session.ExternalID, []db.SessionEvent{{
				UUID: uuid.NewV4().String(), ExternalID: futureID, EventType: "session.error",
				Payload: json.RawMessage(`{"id":` + quoteJSON(futureID) + `,"type":"session.error","error":{"type":"future_error","message":"future message","retry_status":{"type":"future_retry"}}}`),
			}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			after := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			added := after.Data[len(before.Data):]
			wantTypes := []string{"session.error", "session.thread_status_idle", "session.status_idle", "session.error"}
			if len(added) != len(wantTypes) {
				t.Fatalf("unexpected events: %s", added)
			}
			scanner := bufio.NewScanner(stream.Body)
			for i, want := range wantTypes {
				assertSessionEventJSONEqual(t, assertNextSessionFrameType(t, scanner, want), added[i])
			}
			if !strings.Contains(string(added[0]), "9007199254740993") {
				t.Fatal("validation lost unknown numeric precision")
			}
			for _, raw := range added[1:3] {
				var idle struct {
					StopReason struct {
						Type string `json:"type"`
					} `json:"stop_reason"`
				}
				if err := json.Unmarshal(raw, &idle); err != nil || idle.StopReason.Type != "end_turn" {
					t.Fatalf("model stop reason leaked into idle: %s", raw)
				}
			}
			if current := mustSessionRecord(t, app, response.ID); current.Status != "idle" {
				t.Fatalf("Session status = %s, want idle", current.Status)
			}
		})
	}
}
