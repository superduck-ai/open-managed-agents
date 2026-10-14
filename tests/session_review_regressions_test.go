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

func TestSessionCanAcceptNextTurnAfterWorkerReturnsIdle(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("accepted-input-recovery"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	first := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"first"}]}]}`, defaultTestKey)
	if sessionInputProcessedAt(t, first.Data[0]) == "" {
		t.Fatal("first message was not accepted")
	}
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	next := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"next"}]}]}`, defaultTestKey)
	if sessionInputProcessedAt(t, next.Data[0]) == "" {
		t.Fatal("prior accepted message blocked the next turn after idle")
	}
}

func TestNativeTaskToolPermissions(t *testing.T) {
	for _, test := range []struct {
		name, tool, want string
	}{
		{name: "native write disabled", tool: "Write", want: "deny"},
		{name: "native bash asks", tool: "Bash", want: "ask"},
		{name: "native read allowed", tool: "Read", want: "allow"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("subagent-tool-permission"))
			agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"parent","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"bash","permission_policy":{"type":"always_ask"}},{"name":"write","enabled":false}]}]}`)
			env := createEnvironment(t, app, `{"name":"subagent-tool-permission"}`)
			session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			codeID := launchLocalCodeSession(t, app, session.ID)
			epoch := registerCodeSessionWorker(t, app, codeID)
			postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch,
				`{"type":"session.thread_created","uuid":"child-created","session_thread_id":"sthr_child","agent_name":"child","task_id":"child-runtime","tool_use_id":"dispatch-tool"}`,
				`{"type":"control_request","uuid":"child-permission","session_thread_id":"sthr_child","request_id":"child-request","request":{"subtype":"can_use_tool","tool_name":`+quoteJSON(test.tool)+`,"tool_use_id":"child-tool","input":{}}}`))
			events := listThreadEvents(t, app, session.ID, "sthr_child", defaultTestKey)
			tool := sessionEventObjectByType(t, events, "agent.tool_use")
			if tool["evaluated_permission"] != test.want {
				t.Fatalf("permission=%v, want %s", tool["evaluated_permission"], test.want)
			}
			if test.want == "ask" {
				for _, event := range app.workerEvents.Pending(codeID) {
					if event.EventType == "control_response" {
						t.Fatal("asked tool was approved without confirmation")
					}
				}
				return
			}
			_, response := latestCodeSessionControlResponse(t, app, codeID)
			var payload struct {
				Response struct {
					Response struct {
						Behavior string `json:"behavior"`
					} `json:"response"`
				} `json:"response"`
			}
			if err := jsonv2.Unmarshal(response, &payload); err != nil || payload.Response.Response.Behavior != test.want {
				t.Fatalf("control response=%s, want %s, error=%v", response, test.want, err)
			}
		})
	}
}

func TestCustomToolResultIsProcessedOnReceiptAndNotRepublishedOnACK(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("custom-result-immediate"))
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"custom-result-immediate","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"ask_user_question","permission_policy":{"type":"always_ask"}}]}]}`)
	env := createEnvironment(t, app, `{"name":"custom-result-immediate"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, codeID)
	worker, found, err := app.db.GetCodeSession(t.Context(), codeID)
	if err != nil || !found {
		t.Fatalf("load worker: found=%t, error=%v", found, err)
	}
	postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch,
		`{"type":"control_request","uuid":"custom-tool-request","request_id":"custom-request","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","tool_use_id":"custom-tool","input":{"questions":[{"header":"Color","question":"Favorite color?","options":[{"label":"Blue"},{"label":"Green"}]}]}}}`))
	tool := sessionEventObjectByType(t, listSessionEvents(t, app, session.ID, "types[]=agent.custom_tool_use", defaultTestKey), "agent.custom_tool_use")
	toolID, ok := tool["id"].(string)
	if !ok || toolID == "" {
		t.Fatal("custom tool event has no ID")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+worker.SessionExternalID+"/events/stream?beta=true", nil)
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
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.custom_tool_result","custom_tool_use_id":`+quoteJSON(toolID)+`,"content":[{"type":"text","text":`+quoteJSON(`{"Color":"Blue"}`)+`}]}]}`, defaultTestKey)
	inputID := sessionEventStringField(t, sent.Data[0], "id")
	processedAt := sessionInputProcessedAt(t, sent.Data[0])
	if processedAt == "" {
		t.Fatal("custom tool result was not processed on receipt")
	}
	consumePublicInput(t, app, worker, epoch, inputID)
	history := listSessionEvents(t, app, worker.SessionExternalID, "types[]=user.custom_tool_result", defaultTestKey)
	if len(history.Data) != 1 || sessionInputProcessedAt(t, history.Data[0]) != processedAt {
		t.Fatalf("worker ACK changed receipt processing time: %s", history.Data)
	}
	system := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"system.message","content":[{"type":"text","text":"end"}]}]}`, defaultTestKey)
	systemID := sessionEventStringField(t, system.Data[0], "id")
	count := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var event struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatal(err)
		}
		if event.ID == inputID {
			count++
		}
		if event.ID == systemID {
			break
		}
	}
	if err := scanner.Err(); err != nil || count != 1 {
		t.Fatalf("custom result appeared %d times in SSE: %v", count, err)
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
					for scanner.Scan() {
						if strings.Contains(scanner.Text(), `"type":"session.deleted"`) {
							deleted = true
							break
						}
					}
					if !deleted {
						t.Fatalf("missing deletion SSE: %v", scanner.Err())
					}
					for scanner.Scan() {
					}
					if err := scanner.Err(); err != nil {
						t.Fatalf("deletion SSE did not end: %v", err)
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
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"system.message","content":[{"type":"text","text":"one"}]},{"type":"system.message","content":[{"type":"text","text":"two"}]},{"type":"system.message","content":[{"type":"text","text":"three"}]}]}`, defaultTestKey)
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
		if len(ids) != 3 {
			t.Fatalf("%s lost rows: %v", order, ids)
		}
		if order == "desc" {
			slices.Reverse(ids)
		}
		if ids[0] != sessionEventStringField(t, sent.Data[0], "id") || ids[2] != sessionEventStringField(t, sent.Data[2], "id") {
			t.Fatalf("processing time tie order: %v", ids)
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
