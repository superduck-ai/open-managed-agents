package tests

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestMessagesProxyRequestLifecycle(t *testing.T) {
	entered := make(chan string, 8)
	release := make(chan struct{}, 8)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mode := r.Header.Get("X-Lifecycle-Test")
		entered <- mode
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if mode == "http_error" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"type":"error"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Request-Id", "provider-request")
		var out io.Writer = w
		messageID := "msg_test"
		if mode == "persist_retry" {
			messageID = "msg_retry"
		} else if mode == "gzip" {
			messageID = "msg_gzip"
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				compressed := gzip.NewWriter(w)
				defer compressed.Close()
				out = compressed
			}
		}
		_, _ = io.WriteString(out, "data: {\"type\":\"message_start\",\"message\":{\"id\":\""+messageID+"\",\"usage\":{\"input_tokens\":9,\"output_tokens\":0}}}\n\n")
		blockIndex := 0
		if mode == "success" {
			_, _ = io.WriteString(out, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
			blockIndex = 1
		}
		_, _ = fmt.Fprintf(out, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"text\"}}\n\n", blockIndex)
		if mode != "incomplete_response" {
			answer := "done"
			if mode == "persist_retry" {
				answer = strings.Repeat("x", 40_000) + answer
			}
			_, _ = fmt.Fprintf(out, "data: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":4}}\n\ndata: {\"type\":\"message_stop\"}\n\n", blockIndex, answer)
		}
	}))
	defer upstream.Close()
	defer close(release)
	objects := &payloadFaultStore{fakeStore: newFakeStore("request-lifecycle")}
	app := newPayloadIntegrationApp(t, objects)
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Lifecycle", upstream.URL, "provider-key", messagesTestModel)
	agent := createAgent(t, app, `{"model":"`+messagesTestModel+`","name":"request-lifecycle"}`)
	environment := createEnvironment(t, app, `{"name":"request-lifecycle"}`)
	createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
	epoch := registerCodeSessionWorker(t, app, credential.CodeSessionID)
	codeSession, found, err := app.db.GetCodeSession(t.Context(), credential.CodeSessionID)
	if err != nil || !found {
		t.Fatalf("code session: %v", err)
	}
	var completed int
	for _, mode := range []string{"persist_retry", "http_error", "incomplete_response", "cancelled", "success", "gzip"} {
		t.Run(mode, func(t *testing.T) {
			var uploadAttempts int
			if mode == "persist_retry" {
				objects.afterUpload = func(string) error {
					uploadAttempts++
					if uploadAttempts == 1 {
						return errors.New("injected temporary upload failure")
					}
					return nil
				}
				defer func() { objects.afterUpload = nil }()
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(`{"model":"`+messagesTestModel+`","stream":true,"messages":[]}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Api-Key", credential.Token)
			request.Header.Set("X-Lifecycle-Test", mode)
			if mode == "gzip" {
				request.Header.Set("Accept-Encoding", "gzip")
			}
			finished := make(chan string, 1)
			var receivedComplete bool
			go func() {
				response, err := app.client.Do(request)
				if err != nil {
					finished <- ""
					return
				}
				if mode == "persist_retry" {
					body, readErr := io.ReadAll(response.Body)
					receivedComplete = readErr == nil && response.StatusCode == http.StatusOK && strings.Contains(string(body), `"type":"message_stop"`)
				} else {
					_, _ = io.Copy(io.Discard, response.Body)
				}
				_ = response.Body.Close()
				finished <- response.Header.Get("Request-Id")
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not reach provider")
			}
			events := requestLifecycleEvents(t, app, codeSession, completed*2+1)
			start := events[len(events)-1]
			if start.EventType != "span.model_request_start" {
				t.Fatalf("start not persisted before dispatch: %+v", events)
			}
			if mode == "cancelled" {
				cancel()
			} else {
				release <- struct{}{}
			}
			var responseID string
			select {
			case responseID = <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not finish")
			}
			events = requestLifecycleEvents(t, app, codeSession, (completed+1)*2)
			end := events[len(events)-1]
			var payload struct {
				StartID string `json:"model_request_start_id"`
				Error   *struct {
					Type string `json:"type"`
				} `json:"error"`
				Usage struct {
					Input  int `json:"input_tokens"`
					Output int `json:"output_tokens"`
				} `json:"model_usage"`
			}
			if err := json.Unmarshal(end.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if end.EventType != "span.model_request_end" || payload.StartID != start.ExternalID || end.ProcessedAt.Before(start.ProcessedAt) {
				t.Fatalf("invalid span: start=%+v end=%+v", start, end)
			}
			if mode != "cancelled" && responseID != start.ExternalID {
				t.Fatalf("response ID=%q start=%q", responseID, start.ExternalID)
			}
			ok := mode == "success" || mode == "gzip" || mode == "persist_retry"
			if !ok && (payload.Error == nil || payload.Error.Type != mode) {
				t.Fatalf("error=%+v, want %s", payload.Error, mode)
			}
			if ok && (payload.Error != nil || payload.Usage.Input != 9 || payload.Usage.Output != 4) {
				t.Fatalf("%s usage/error=%+v", mode, payload)
			}
			if mode == "persist_retry" {
				if !receivedComplete || uploadAttempts != 2 || len(events) != (completed+1)*2 {
					t.Fatalf("complete=%v end retry attempts=%d lifecycle events=%d", receivedComplete, uploadAttempts, len(events))
				}
				current, found, err := app.db.GetSession(t.Context(), codeSession.WorkspaceUUID, codeSession.SessionExternalID)
				if err != nil || !found {
					t.Fatalf("session after retry: found=%v err=%v", found, err)
				}
				var usage struct {
					Input  int `json:"input_tokens"`
					Output int `json:"output_tokens"`
				}
				if err := json.Unmarshal(current.Usage, &usage); err != nil || usage.Input != 9 || usage.Output != 4 {
					t.Fatalf("retry usage=%s err=%v", current.Usage, err)
				}
			}
			if mode == "incomplete_response" {
				postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%q,"events":[{"payload":{"type":"assistant","uuid":"fallback-answer","request_id":%q,"message":{"id":"msg_test","content":[{"type":"text","text":"worker fallback"}]}}}]}`, epoch, start.ExternalID))
				history := listSessionEvents(t, app, codeSession.SessionExternalID, "order=asc&limit=100", defaultTestKey)
				var fallbackCount int
				for _, raw := range history.Data {
					if sessionEventStringField(t, raw, "type") == "agent.message" && sessionEventStringField(t, raw, "model_request_start_id") == start.ExternalID {
						fallbackCount++
					}
				}
				if fallbackCount != 1 {
					t.Fatalf("worker fallback messages = %d, want 1", fallbackCount)
				}
			}
			if mode == "success" {
				postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%q,"events":[{"payload":{"type":"assistant","uuid":"late-answer","request_id":%q,"message":{"id":"msg_test","content":[{"type":"text","text":"done"}]} }},{"payload":{"type":"result","uuid":"turn-result","duration_api_ms":900000,"usage":{"output_tokens":9999}}}]}`, epoch, start.ExternalID))
				history := listSessionEvents(t, app, codeSession.SessionExternalID, "order=asc&limit=100", defaultTestKey)
				finals := 0
				for i, raw := range history.Data {
					if sessionEventStringField(t, raw, "id") == end.ExternalID {
						if i == 0 || sessionEventStringField(t, history.Data[i-1], "type") != "agent.message" || sessionEventStringField(t, history.Data[i-1], "model_request_start_id") != start.ExternalID {
							t.Fatal("final message must precede its request end")
						}
					}
					if sessionEventStringField(t, raw, "type") == "agent.message" && sessionEventStringField(t, raw, "model_request_start_id") == start.ExternalID {
						finals++
						if id := sessionEventStringField(t, raw, "id"); id != maevents.StableAssistantEventID(codeSession.ExternalID, "msg_test", 1, "agent.message") {
							t.Fatalf("final message id = %s, want original text block index 1", id)
						}
						if !strings.Contains(string(raw), "done") {
							t.Fatalf("proxy lost final text: %s", raw)
						}
					}
				}
				if finals != 1 {
					t.Fatalf("worker echo duplicated final message: %d", finals)
				}
				// The worker's late result must not fabricate another request.
				events = requestLifecycleEvents(t, app, codeSession, (completed+1)*2)
				if len(events) != (completed+1)*2 {
					t.Fatalf("result fabricated spans: %d", len(events))
				}
			}
			completed++
		})
	}
	t.Run("concurrent child waits for its task mapping", func(t *testing.T) {
		send := func(agentID, mode string) <-chan string {
			done := make(chan string, 1)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(`{"model":"`+messagesTestModel+`","stream":true,"messages":[]}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Api-Key", credential.Token)
			request.Header.Set("X-Lifecycle-Test", mode)
			request.Header.Set("X-Claude-Code-Agent-Id", agentID)
			go func() {
				response, err := app.client.Do(request)
				if err != nil {
					done <- ""
					return
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				done <- response.Header.Get("Request-Id")
			}()
			return done
		}
		child := send("task_lifecycle", "child")
		select {
		case <-entered:
			t.Fatal("unmapped child reached provider")
		case <-time.After(80 * time.Millisecond):
		}
		main := send("", "main")
		select {
		case mode := <-entered:
			if mode != "main" {
				t.Fatalf("unexpected request %s", mode)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("main did not reach provider")
		}
		postCodeSessionWorkerEvents(t, app, codeSession.ExternalID, fmt.Sprintf(`{"worker_epoch":%q,"events":[{"payload":{"type":"system","uuid":"task-lifecycle","subtype":"task_started","task_id":"task_lifecycle","tool_use_id":"tool_lifecycle","description":"Child"}}]}`, epoch))
		select {
		case mode := <-entered:
			if mode != "child" {
				t.Fatalf("unexpected request %s", mode)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("mapped child did not reach provider")
		}
		release <- struct{}{}
		release <- struct{}{}
		mainID, childID := <-main, <-child
		if mainID == "" || childID == "" || mainID == childID {
			t.Fatalf("request IDs: main=%q child=%q", mainID, childID)
		}
		events := requestLifecycleEvents(t, app, codeSession, completed*2+4)
		for _, event := range events {
			if event.ExternalID == childID || event.ExternalID == childID+"_end" {
				if event.ThreadExternalID == nil || *event.ThreadExternalID != maevents.ClaudeTaskThreadID(codeSession.ExternalID, "tool_lifecycle") {
					t.Fatalf("child event has wrong owner: %+v", event)
				}
			}
		}
	})
	t.Run("history preserves insertion order at equal processed times", func(t *testing.T) {
		at := time.Now().UTC().Truncate(time.Microsecond)
		eventType := "test.order." + codeSession.ExternalID
		events := []db.SessionEvent{}
		for index, id := range []string{"c", "b", "a"} {
			events = append(events, db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: codeSession.ExternalID + id, EventType: eventType, Payload: json.RawMessage(`{}`), CreatedAt: at.Add(time.Duration(index) * time.Hour), ProcessedAt: at})
		}
		if _, err := app.db.AppendSessionEvents(t.Context(), codeSession.WorkspaceUUID, codeSession.SessionExternalID, events, nil); err != nil {
			t.Fatal(err)
		}
		params := db.ListSessionEventsPageParams{WorkspaceUUID: codeSession.WorkspaceUUID, SessionExternalID: codeSession.SessionExternalID, Order: "asc", Limit: 2, Types: []string{eventType}}
		first, more, err := app.db.ListSessionEventsPage(t.Context(), params)
		if err != nil || !more || len(first) != 2 || first[0].ExternalID != codeSession.ExternalID+"c" || first[1].ExternalID != codeSession.ExternalID+"b" {
			t.Fatalf("first page=%+v more=%v err=%v", first, more, err)
		}
		params.Cursor = &db.SessionEventPageCursor{ExternalID: first[1].ExternalID}
		next, more, err := app.db.ListSessionEventsPage(t.Context(), params)
		if err != nil || more || len(next) != 1 || next[0].ExternalID != codeSession.ExternalID+"a" {
			t.Fatalf("next page=%+v more=%v err=%v", next, more, err)
		}
		params.Order = "desc"
		previous, more, err := app.db.ListSessionEventsPage(t.Context(), params)
		if err != nil || more || len(previous) != 1 || previous[0].ExternalID != codeSession.ExternalID+"c" {
			t.Fatalf("previous page=%+v more=%v err=%v", previous, more, err)
		}
		params.Cursor = nil
		params.CreatedAtGTE, params.CreatedAtLTE = &at, &at
		params.Limit = 10
		filtered, _, err := app.db.ListSessionEventsPage(t.Context(), params)
		if err != nil || len(filtered) != 1 || filtered[0].ExternalID != codeSession.ExternalID+"c" {
			t.Fatalf("created_at filter must compare created_at: %+v %v", filtered, err)
		}

	})
	t.Run("archived session rejects lifecycle persistence", func(t *testing.T) {
		if err := app.db.SetSessionStatus(t.Context(), codeSession.WorkspaceUUID, codeSession.SessionExternalID, "idle"); err != nil {
			t.Fatal(err)
		}
		archiveSession(t, app, codeSession.SessionExternalID)
		sink := sessionsapi.NewHandler(app.cfg, app.db, newCodeSessionService(app, nil, nil), nil, nil, app.vaultSecrets, nil)
		err := sink.PublishCodeSessionEvents(t.Context(), codeSession, []json.RawMessage{json.RawMessage(`{"id":"sevt_archived","type":"span.model_request_start"}`)})
		if !errors.Is(err, db.ErrInvalidState) {
			t.Fatalf("archived lifecycle write error=%v, want invalid state", err)
		}
	})
}

func requestLifecycleEvents(t *testing.T, app *testApp, session db.CodeSession, count int) []db.SessionEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		events, _, err := app.db.ListSessionEventsPage(t.Context(), db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.SessionExternalID, Order: "asc", Limit: 100, CreatedAtGTE: &session.CreatedAt, Types: []string{"span.model_request_start", "span.model_request_end"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) >= count {
			return events
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d lifecycle events, want %d", len(events), count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
