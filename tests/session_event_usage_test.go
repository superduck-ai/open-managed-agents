package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestWorkerUsageSnapshotConsistency(t *testing.T) {
	for _, endpoint := range []string{"worker", "legacy"} {
		t.Run(endpoint, func(t *testing.T) {
			app, agent, env := newSessionEventTestApp(t, "session-usage", `{"model":"claude-opus-4-6","name":"usage"}`, `{"name":"usage"}`)
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			session := mustSessionRecord(t, app, response.ID)
			codeID := launchLocalCodeSession(t, app, response.ID)
			epoch := registerCodeSessionWorker(t, app, codeID)
			post := func(payloads ...string) *http.Response {
				t.Helper()
				for i, payload := range payloads {
					payloads[i] = strings.ReplaceAll(payload, `"usage-`, `"`+response.ID+`-usage-`)
				}
				if endpoint == "worker" {
					events := make([]string, len(payloads))
					for i, payload := range payloads {
						events[i] = `{"payload":` + payload + `}`
					}
					return doCodeSessionWorkerRequest(t, app, codeID, "events", `{"worker_epoch":`+epoch+`,"events":[`+strings.Join(events, ",")+`]}`)
				}
				return doSessionEventIngressRequest(t, app, http.MethodPost, codeID, "/events", `{"events":[`+strings.Join(payloads, ",")+`]}`)
			}
			postOK := func(payloads ...string) {
				t.Helper()
				resp := post(payloads...)
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("post status = %d: %s", resp.StatusCode, readAll(t, resp.Body))
				}
			}
			before := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			clockBefore, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			firstUsage := `{"input_tokens":3,"output_tokens":0,"list_cost":{"amount":"9007199254740993","currency":"USD"},"future_counter":9007199254740993}`
			first := `{"id":"usage-first","uuid":"usage-first","type":"session.usage","usage":` + firstUsage + `,"budget":null}`
			last := `{"id":"usage-last","uuid":"usage-last","type":"agent.message","content":[{"type":"text","text":"complete"}]}`
			for _, invalid := range []string{
				`{"uuid":"usage-invalid","type":"session.usage","usage":null}`,
				`{"uuid":"usage-invalid","type":"session.usage","usage":{"input_tokens":null}}`,
				`{"uuid":"usage-invalid","type":"session.usage","usage":{},"owner_session_thread_id":"sthr_invalid"}`,
			} {
				assertError(t, post(first, invalid), http.StatusBadRequest, "invalid_request_error")
			}
			constraint := `"test_usage_failure_` + uuid.NewV4().String() + `"`
			if _, err := app.pool.Exec(t.Context(), `ALTER TABLE sessions ADD CONSTRAINT `+constraint+` CHECK (uuid <> '`+session.UUID+`' OR usage = '{}'::jsonb)`); err != nil {
				t.Fatal(err)
			}
			removeConstraint := func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := app.pool.Exec(ctx, `ALTER TABLE sessions DROP CONSTRAINT IF EXISTS `+constraint); err != nil {
					t.Error(err)
				}
			}
			defer removeConstraint()
			assertError(t, post(first), http.StatusInternalServerError, "api_error")
			removeConstraint()
			remove := rejectPublicSessionEventWrites(t, app, session.UUID, "agent.message")
			defer remove()
			assertError(t, post(first, last), http.StatusInternalServerError, "api_error")
			remove()
			if got := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey); !reflect.DeepEqual(got.Data, before.Data) {
				t.Fatal("failed batch committed events")
			}
			assertSessionUsageSnapshot(t, app, response.ID, `{}`)
			clockAfter, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !clockAfter.Equal(clockBefore) {
				t.Fatalf("failed batch advanced clock: %v", err)
			}
			if threads := listSessionThreads(t, app, response.ID, defaultTestKey); len(threads.Data) != 1 {
				t.Fatal("invalid usage created a thread")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			stream := openSessionEventStream(t, app, ctx, "/v1/sessions/"+response.ID+"/events/stream?beta=true")
			defer stream.Body.Close()
			postOK(first, last)
			assertSessionUsageSnapshot(t, app, response.ID, firstUsage)
			second := `{"id":"usage-second","uuid":"usage-second","type":"session.usage","usage":{"output_tokens":7}}`
			postOK(second)
			postOK(first)
			assertSessionUsageSnapshot(t, app, response.ID, `{"output_tokens":7}`)
			if endpoint == "worker" {
				assertError(t, post(`{"id":"usage-conflict-prefix","uuid":"usage-conflict-prefix","type":"session.usage","usage":{}}`, strings.Replace(first, `"input_tokens":3`, `"input_tokens":4`, 1)), http.StatusConflict, "conflict_error")
				assertSessionUsageSnapshot(t, app, response.ID, `{"output_tokens":7}`)
			}
			// An explicit unknown snapshot replaces the prior fields without filling zeros.
			postOK(`{"id":"usage-unknown","uuid":"usage-unknown","type":"session.usage","usage":{}}`)
			assertSessionUsageSnapshot(t, app, response.ID, `{}`)
			after := listSessionEvents(t, app, response.ID, "limit=100", defaultTestKey)
			if len(after.Data) != len(before.Data)+4 {
				t.Fatalf("history count = %d", len(after.Data))
			}
			scanner := bufio.NewScanner(stream.Body)
			for _, raw := range after.Data[len(before.Data):] {
				assertSessionEventJSONEqual(t, assertNextSessionFrameType(t, scanner, sessionEventStringField(t, raw, "type")), raw)
			}
			if current := mustSessionRecord(t, app, response.ID); current.Status != session.Status {
				t.Fatal("usage changed Session status")
			}
			// Scope is enforced even if a caller passes a valid UUID in another workspace.
			err = app.db.WithManagedAgentEventTx(t.Context(), func(tx db.ManagedAgentEventTx) error {
				locked, err := tx.LockSessionForEvents(t.Context(), session.WorkspaceUUID, session.ExternalID)
				if err != nil {
					return err
				}
				locked.WorkspaceUUID = uuid.NewV4().String()
				return tx.SetSessionUsage(t.Context(), locked, json.RawMessage(`{"output_tokens":9}`))
			})
			if !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("cross workspace usage write = %v", err)
			}
			assertSessionUsageSnapshot(t, app, response.ID, `{}`)
		})
	}
}

func assertSessionUsageSnapshot(t *testing.T, app *testApp, sessionID, want string) {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+sessionID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve status = %d", resp.StatusCode)
	}
	var body struct {
		Usage json.RawMessage `json:"usage"`
	}
	decodeJSON(t, resp.Body, &body)
	assertSessionEventJSONEqual(t, body.Usage, json.RawMessage(want))
	assertSessionEventJSONEqual(t, mustSessionRecord(t, app, sessionID).Usage, body.Usage)
}
