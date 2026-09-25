package tests

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionUnacknowledgedInputsBlockAcceptance(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("pending-inputs"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	for _, input := range []string{
		`{"type":"user.tool_confirmation","tool_use_id":"sevt_tool","result":"allow"}`,
		`{"type":"user.custom_tool_result","custom_tool_use_id":"sevt_tool","content":[{"type":"text","text":"test"}]}`,
		`{"type":"user.interrupt"}`,
	} {
		sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[`+input+`]}`, defaultTestKey)
		if sessionInputProcessedAt(t, sent.Data[0]) != "" {
			t.Fatal("worker input processed before ACK")
		}
		next := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"test"}]}]}`, defaultTestKey)
		if sessionInputProcessedAt(t, next.Data[0]) != "" {
			t.Fatal("unacknowledged input did not block acceptance")
		}
		assertQueuedInputPreservesIdle(t, app, worker)
		for _, raw := range []json.RawMessage{sent.Data[0], next.Data[0]} {
			if _, changed, err := app.db.MarkSessionEventProcessed(t.Context(), worker, sessionEventStringField(t, raw, "id"), time.Now().UTC()); err != nil || !changed {
				t.Fatalf("process pending input: %t %v", changed, err)
			}
		}
	}
	batch := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.custom_tool_result","custom_tool_use_id":"sevt_tool","content":[{"type":"text","text":"test"}]},{"type":"user.message","content":[{"type":"text","text":"test"}]}]}`, defaultTestKey)
	if sessionInputProcessedAt(t, batch.Data[1]) != "" {
		t.Fatal("earlier control input in the same batch did not block acceptance")
	}
}

func TestSessionGenericRequiresAction(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("generic-requires-action"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	for _, extra := range []string{``, `,"requires_action_details":{"reason":"pending_action"}`} {
		putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
		putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"requires_action"`+extra+`}`)
		if got := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; got != "idle" {
			t.Fatalf("blocked worker left session %s", got)
		}
		primary, found, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
		if err != nil || !found || primary.Status != "idle" {
			t.Fatalf("primary: %+v %v", primary, err)
		}
		events := listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.status_idle&limit=1", defaultTestKey)
		if strings.Contains(string(events.Data[0]), "requires_action") {
			t.Fatal("invented tool wait without pending tools")
		}
	}
}

func TestSessionExplicitThreadStatusOwnership(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("status-ownership"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	child := "sthr_child_" + worker.SessionExternalID
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.thread_status_running","uuid":"owned-running","id":"sevt_owned_running","session_thread_id":`+quoteJSON(child)+`,"owner_session_thread_id":`+quoteJSON(child)+`}`))
	events := listThreadEvents(t, app, worker.SessionExternalID, child, defaultTestKey)
	if len(events.Data) != 1 || sessionEventStringField(t, events.Data[0], "id") != "sevt_owned_running" {
		t.Fatalf("child history lost owned status: %s", events.Data)
	}
	primary := listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.thread_status_running", defaultTestKey)
	if len(primary.Data) != 0 {
		t.Fatalf("owned status leaked into primary: %s", primary.Data)
	}
	// Without explicit ownership, the coordination status still belongs to primary.
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.thread_status_idle","uuid":"coordination-idle","session_thread_id":`+quoteJSON(child)+`}`))
	primary = listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.thread_status_idle", defaultTestKey)
	if len(primary.Data) != 1 {
		t.Fatalf("missing primary coordination status: %s", primary.Data)
	}
}

func TestSessionRemovalBeforeWorkerStarts(t *testing.T) {
	for _, state := range []string{"running", "unstarted", "absent"} {
		for _, operation := range []string{"archive", "delete"} {
			t.Run(state+"/"+operation, func(t *testing.T) {
				app := newPayloadIntegrationApp(t, newFakeStore("remove-unstarted"))
				agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"remove-unstarted"}`)
				env := createEnvironment(t, app, `{"name":"remove-unstarted"}`)
				session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
				codeID, epoch := "", ""
				if state != "absent" {
					codeID = launchLocalCodeSession(t, app, session.ID)
					epoch = registerCodeSessionWorker(t, app, codeID)
				}
				sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"test"}]}]}`, defaultTestKey)
				if state == "running" {
					putCodeSessionWorkerState(t, app, codeID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
				}
				method, path := http.MethodDelete, "/v1/sessions/"+session.ID+"?beta=true"
				if operation == "archive" {
					method, path = http.MethodPost, "/v1/sessions/"+session.ID+"/archive?beta=true"
				}
				var stream *http.Response
				if operation == "delete" && state != "running" {
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+session.ID+"/events/stream?beta=true", nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("X-Api-Key", defaultTestKey)
					req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
					stream, err = app.client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer stream.Body.Close()
				}

				resp := doSessionRequest(t, app, method, path, nil, defaultTestKey, true)
				if state == "running" {
					assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
					if got := retrieveSession(t, app, session.ID, defaultTestKey).Status; got != "running" {
						t.Fatalf("rejected removal changed status: %s", got)
					}
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("remove unstarted: %d %s", resp.StatusCode, readAll(t, resp.Body))
				}
				if stream != nil {
					scanner := bufio.NewScanner(stream.Body)
					deleted := false
					frameID := ""
					for scanner.Scan() {
						line := scanner.Text()
						if line == "" {
							frameID = ""
						} else if strings.HasPrefix(line, "id: ") {
							frameID = strings.TrimPrefix(line, "id: ")
						}
						if strings.Contains(line, `"type":"session.deleted"`) {
							if frameID != "" {
								t.Fatalf("deleted notification advanced SSE cursor: %q", frameID)
							}
							deleted = true
							break
						}
					}
					if !deleted {
						t.Fatalf("missing deletion SSE: %v", scanner.Err())
					}
				}

				if codeID != "" {
					worker, found, err := app.db.GetCodeSession(t.Context(), codeID)
					if err != nil || !found || worker.Status != "terminated" || worker.WorkerLeaseExpiresAt != nil {
						t.Fatalf("worker not revoked: status=%s err=%v", worker.Status, err)
					}
				}
				if operation == "archive" {
					if retrieveSession(t, app, session.ID, defaultTestKey).Status != "terminated" {
						t.Fatal("archived unstarted turn still running")
					}
					terminated := listSessionEvents(t, app, session.ID, "types[]=session.status_terminated&types[]=session.thread_status_terminated", defaultTestKey)
					if len(terminated.Data) != 2 {
						t.Fatalf("archived unstarted turn status history = %s", terminated.Data)
					}
				}
			})
		}
	}
}

func TestSessionHistoryCursorBoundaries(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("cursor-boundaries"))
	worker, _ := newPayloadIntegrationSession(t, app)
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"test"}]},{"type":"user.message","content":[{"type":"text","text":"test"}]},{"type":"user.message","content":[{"type":"text","text":"test"}]}]}`, defaultTestKey)
	for _, order := range []string{"asc", "desc"} {
		var cursor *db.SessionEventPageCursor
		var ids []string
		for {
			events, more, err := app.db.ListSessionEventsPage(t.Context(), db.ListSessionEventsPageParams{WorkspaceUUID: worker.WorkspaceUUID, SessionExternalID: worker.SessionExternalID, Order: order, Limit: 1, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 {
				t.Fatalf("page: %+v", events)
			}
			if slices.Contains(ids, events[0].ExternalID) {
				t.Fatal("repeated row")
			}
			ids = append(ids, events[0].ExternalID)
			if !more {
				break
			}
			cursor = &db.SessionEventPageCursor{ExternalID: events[0].ExternalID}
		}
		if len(ids) != 5 {
			t.Fatalf("%s lost rows: %v", order, ids)
		}
		if order == "desc" {
			slices.Reverse(ids)
		}
		if ids[2] != sessionEventStringField(t, sent.Data[0], "id") || ids[4] != sessionEventStringField(t, sent.Data[2], "id") {
			t.Fatalf("processed/null tie order: %v", ids)
		}
	}
	firstID := sessionEventStringField(t, sent.Data[0], "id")
	cursorFor := func(id string) string {
		raw, err := jsonv2.Marshal(map[string]string{"external_id": id, "processed_at": sessionInputProcessedAt(t, sent.Data[0])})
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+worker.SessionExternalID+"/events?beta=true&page="+cursorFor("sevt_missing"), nil, defaultTestKey, true)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	// Soft deletion must not invalidate an already issued cursor.
	if _, err := app.pool.Exec(t.Context(), `UPDATE session_events SET deleted_at=now() WHERE external_id=$1`, firstID); err != nil {
		t.Fatal(err)
	}
	page := listSessionEvents(t, app, worker.SessionExternalID, "order=asc&page="+cursorFor(firstID), defaultTestKey)
	if len(page.Data) != 2 {
		t.Fatalf("soft-deleted cursor: %s", page.Data)
	}
	if _, err := app.pool.Exec(t.Context(), `DELETE FROM session_events WHERE external_id=$1`, firstID); err != nil {
		t.Fatal(err)
	}
	resp = doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+worker.SessionExternalID+"/events?beta=true&page="+cursorFor(firstID), nil, defaultTestKey, true)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
}
