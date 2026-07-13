package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
)

type sessionAPIResponse struct {
	ID                 string            `json:"id"`
	Type               string            `json:"type"`
	EnvironmentID      string            `json:"environment_id"`
	DeploymentID       *string           `json:"deployment_id"`
	Status             string            `json:"status"`
	Title              *string           `json:"title"`
	Metadata           json.RawMessage   `json:"metadata"`
	Resources          []json.RawMessage `json:"resources"`
	OutcomeEvaluations json.RawMessage   `json:"outcome_evaluations"`
	ArchivedAt         *string           `json:"archived_at"`
	CreatedAt          string            `json:"created_at"`
	UpdatedAt          string            `json:"updated_at"`
}

type sessionPageAPIResponse struct {
	Data     []sessionAPIResponse `json:"data"`
	NextPage *string              `json:"next_page"`
}

type sessionThreadAPIResponse struct {
	ID             string  `json:"id"`
	Type           string  `json:"type"`
	SessionID      string  `json:"session_id"`
	Status         string  `json:"status"`
	ParentThreadID *string `json:"parent_thread_id"`
	ArchivedAt     *string `json:"archived_at"`
}

type sessionThreadPageAPIResponse struct {
	Data     []sessionThreadAPIResponse `json:"data"`
	NextPage *string                    `json:"next_page"`
}

type sessionEventPageAPIResponse struct {
	Data     []json.RawMessage `json:"data"`
	NextPage *string           `json:"next_page"`
}

type sessionEventSendAPIResponse struct {
	Data []json.RawMessage `json:"data"`
}

func TestSessionsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-bucket"))
	defer app.close()

	t.Run("success missing beta header", func(t *testing.T) {
		resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions?beta=true", nil, defaultTestKey, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("failure missing beta query", func(t *testing.T) {
		resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success lifecycle resources threads events work and archive", func(t *testing.T) {
		agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-api-agent"}`)
		defer cleanupAgentRows(t, app.db, agent.ID)
		env := createEnvironment(t, app, `{"name":"sessions-api-env"}`)
		defer cleanupEnvironmentRows(t, app.db, env.ID)
		file := uploadFile(t, app, "session-resource.txt", "text/plain", []byte("session file"))
		defer deleteFile(t, app, file.ID)
		memoryStore := createMemoryStore(t, app, "sessions-api-memory")
		defer deleteMemoryStore(t, app, memoryStore.ID)

		created := createSession(t, app, `{
			"agent":`+quoteJSON(agent.ID)+`,
			"environment_id":`+quoteJSON(env.ID)+`,
			"title":"Order #1234 inquiry",
			"metadata":{"case":"1234"},
			"resources":[
				{"type":"file","file_id":`+quoteJSON(file.ID)+`,"mount_path":"/mnt/data/session-resource.txt"},
				{"type":"memory_store","memory_store_id":`+quoteJSON(memoryStore.ID)+`,"name":"memory"}
			]
		}`)
		if created.Type != "session" || created.Status != "idle" || created.EnvironmentID != env.ID {
			t.Fatalf("unexpected created session: %+v", created)
		}
		if created.Title == nil || *created.Title != "Order #1234 inquiry" {
			t.Fatalf("created title = %v", created.Title)
		}
		if len(created.Resources) != 2 {
			t.Fatalf("created resources len = %d, want 2", len(created.Resources))
		}
		assertRawContains(t, created.Metadata, `"case":"1234"`)

		workType, workSessionID, workState := sessionWorkData(t, app, created.ID)
		if workType != "session" || workSessionID != created.ID || workState != "queued" {
			t.Fatalf("unexpected session work type=%s session_id=%s state=%s", workType, workSessionID, workState)
		}

		retrieved := retrieveSession(t, app, created.ID, defaultTestKey)
		if retrieved.ID != created.ID || retrieved.Status != "idle" {
			t.Fatalf("unexpected retrieved session: %+v", retrieved)
		}
		listed := listSessions(t, app, "agent_id="+url.QueryEscape(agent.ID))
		if !containsSession(listed.Data, created.ID) {
			t.Fatalf("session list missing created session: %+v", listed.Data)
		}

		threads := listSessionThreads(t, app, created.ID, defaultTestKey)
		if len(threads.Data) != 1 || threads.Data[0].SessionID != created.ID || threads.Data[0].Status != "idle" {
			t.Fatalf("unexpected session threads: %+v", threads)
		}
		thread := retrieveSessionThread(t, app, created.ID, threads.Data[0].ID, defaultTestKey)
		if thread.ID != threads.Data[0].ID {
			t.Fatalf("retrieve thread id = %s, want %s", thread.ID, threads.Data[0].ID)
		}

		sent := sendSessionEvents(t, app, created.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`, defaultTestKey)
		if len(sent.Data) != 1 || !bytes.Contains(sent.Data[0], []byte(`"type":"user.message"`)) {
			t.Fatalf("unexpected sent events: %+v", sent)
		}
		sentEventID := sessionEventStringField(t, sent.Data[0], "id")
		sentCreatedAt := sessionEventStringField(t, sent.Data[0], "created_at")
		if _, err := time.Parse(time.RFC3339, sentCreatedAt); err != nil {
			t.Fatalf("sent event created_at = %q, want RFC3339: %v", sentCreatedAt, err)
		}
		if _, err := app.db.Pool.Exec(context.Background(), `update session_events set payload = payload - 'created_at' where external_id = $1`, sentEventID); err != nil {
			t.Fatalf("remove stored event created_at: %v", err)
		}
		events := listSessionEvents(t, app, created.ID, "", defaultTestKey)
		if len(events.Data) != 1 || !bytes.Contains(events.Data[0], []byte(`"id":"sevt_`)) {
			t.Fatalf("unexpected listed events: %+v", events)
		}
		if listedCreatedAt := sessionEventStringField(t, events.Data[0], "created_at"); listedCreatedAt != sentCreatedAt {
			t.Fatalf("listed event created_at = %q, want %q", listedCreatedAt, sentCreatedAt)
		}
		threadEvents := listThreadEvents(t, app, created.ID, thread.ID, defaultTestKey)
		if len(threadEvents.Data) != 1 {
			t.Fatalf("unexpected thread events: %+v", threadEvents)
		}

		updated := updateSession(t, app, created.ID, `{"title":"updated","metadata":{"case":"","priority":"high"},"agent":{"tools":[],"mcp_servers":[]}}`)
		if updated.Title == nil || *updated.Title != "updated" {
			t.Fatalf("updated title = %v", updated.Title)
		}
		assertRawContains(t, updated.Metadata, `"priority":"high"`)
		assertRawNotContains(t, updated.Metadata, `"case"`)

		archivedThread := archiveSessionThread(t, app, created.ID, thread.ID)
		if archivedThread.ArchivedAt == nil {
			t.Fatalf("archived thread archived_at = nil")
		}
		archived := archiveSession(t, app, created.ID)
		if archived.ArchivedAt == nil {
			t.Fatalf("archived session archived_at = nil")
		}
		resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+created.ID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.interrupt"}]}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		deleteSession(t, app, created.ID)
		resp = doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+created.ID+"?beta=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})
}

func TestSessionsEnvironmentKeyAccess(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-env-key-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-env-key-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-env-key-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	otherEnv := createEnvironment(t, app, `{"name":"sessions-env-key-other-env"}`)
	defer cleanupEnvironmentRows(t, app.db, otherEnv.ID)

	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	otherSession := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(otherEnv.ID)+`}`)
	envKey := createEnvironmentKeyForTest(t, app, env.ID, "sk-ant-env-session-test")

	got := retrieveSession(t, app, session.ID, envKey)
	if got.ID != session.ID {
		t.Fatalf("env key retrieve session id = %s, want %s", got.ID, session.ID)
	}
	events := sendSessionEvents(t, app, session.ID, `{"events":[{"type":"system.message","content":[{"type":"text","text":"worker note"}]}]}`, envKey)
	if len(events.Data) != 1 {
		t.Fatalf("env key send events = %+v", events)
	}

	resp := doSessionBearerRequest(t, app, http.MethodGet, "/v1/sessions/"+otherSession.ID+"?beta=true", nil, envKey, true)
	assertError(t, resp, http.StatusForbidden, "permission_error")

	resp = doSessionBearerRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"?beta=true", strings.NewReader(`{"title":"nope"}`), envKey, true)
	assertError(t, resp, http.StatusForbidden, "permission_error")

	resp = doSessionBearerRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/resources?beta=true", strings.NewReader(`{"type":"memory_store","memory_store_id":"mem_denied"}`), envKey, true)
	assertError(t, resp, http.StatusForbidden, "permission_error")
}

func TestSessionsPlatformSessionAccess(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-platform-session-bucket"))
	defer app.close()

	cookies := app.platformLoginCookies(t, "sessions-platform@example.com")
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-platform-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-platform-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)

	createBody := `{"environment_id":` + quoteJSON(env.ID) + `,"agent":{"type":"agent","id":` + quoteJSON(agent.ID) + `}}`
	createResp := app.platformRequest(t, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(createBody), cookies)
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("platform create session status = %d, want 200: %s", createResp.StatusCode, readAll(t, createResp.Body))
	}
	var created sessionAPIResponse
	decodeJSON(t, createResp.Body, &created)
	if created.ID == "" || created.EnvironmentID != env.ID {
		t.Fatalf("platform created session = %+v, want environment %s", created, env.ID)
	}

	listResp := app.platformRequest(t, http.MethodGet, "/v1/sessions?beta=true&agent_id="+url.QueryEscape(agent.ID), nil, cookies)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("platform list sessions status = %d, want 200: %s", listResp.StatusCode, readAll(t, listResp.Body))
	}
	var listed sessionPageAPIResponse
	decodeJSON(t, listResp.Body, &listed)
	if !containsSession(listed.Data, created.ID) {
		t.Fatalf("platform list missing created session: %+v", listed.Data)
	}

	retrieveResp := app.platformRequest(t, http.MethodGet, "/v1/sessions/"+created.ID+"?beta=true", nil, cookies)
	defer retrieveResp.Body.Close()
	if retrieveResp.StatusCode != http.StatusOK {
		t.Fatalf("platform retrieve session status = %d, want 200: %s", retrieveResp.StatusCode, readAll(t, retrieveResp.Body))
	}
	var retrieved sessionAPIResponse
	decodeJSON(t, retrieveResp.Body, &retrieved)
	if retrieved.ID != created.ID {
		t.Fatalf("platform retrieve id = %s, want %s", retrieved.ID, created.ID)
	}

	updateResp := app.platformRequest(t, http.MethodPost, "/v1/sessions/"+created.ID+"?beta=true", strings.NewReader(`{"title":"platform updated"}`), cookies)
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("platform update session status = %d, want 200: %s", updateResp.StatusCode, readAll(t, updateResp.Body))
	}
	var updated sessionAPIResponse
	decodeJSON(t, updateResp.Body, &updated)
	if updated.Title == nil || *updated.Title != "platform updated" {
		t.Fatalf("platform updated title = %v, want platform updated", updated.Title)
	}
}

func TestPlatformWebSessionStream(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-platform-web-stream-bucket"))
	defer app.close()

	cookies := app.platformLoginCookies(t, "sessions-platform-web-stream@example.com")
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-platform-web-stream-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-platform-web-stream-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)

	createBody := `{"environment_id":` + quoteJSON(env.ID) + `,"agent":{"type":"agent","id":` + quoteJSON(agent.ID) + `}}`
	createResp := app.platformRequest(t, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(createBody), cookies)
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("platform create session status = %d, want 200: %s", createResp.StatusCode, readAll(t, createResp.Body))
	}
	var session sessionAPIResponse
	decodeJSON(t, createResp.Body, &session)

	unauthResp := app.platformRequest(t, http.MethodGet, "/web-api/sessions/"+session.ID+"/stream", nil, nil)
	assertError(t, unauthResp, http.StatusUnauthorized, "authentication_error")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/web-api/sessions/"+session.ID+"/stream", nil)
	if err != nil {
		t.Fatalf("new platform web stream request: %v", err)
	}
	req.Host = "platform.claude.com"
	req.Header.Set("Accept", "text/event-stream")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("open platform web stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("platform web stream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("platform web stream content-type = %q, want text/event-stream", contentType)
	}

	lineCh := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
	}()

	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"hello from web-api stream"}]}]}`, defaultTestKey)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				t.Fatal("platform web stream closed before event arrived")
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "hello from web-api stream") {
				if !strings.Contains(line, `"created_at"`) {
					t.Fatalf("streamed event missing created_at: %s", line)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for platform web stream event")
		}
	}
}

func TestSessionEventsFromCodeSessionIngress(t *testing.T) {
	var (
		mu       sync.Mutex
		requests int
	)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.WebhookEndpointURL = receiver.URL
	cfg.WebhookSigningKey = "whsec_c2VjcmV0Cg=="
	cfg.WebhookEventTypes = []string{"session.status_idled"}
	cfg.WebhookWorkerEnabled = true
	cfg.WebhookAllowInsecure = true

	app := newTestAppWithStore(t, &cfg, newFakeStore("sessions-events-ingress-bucket"))
	defer app.close()
	clearWebhookState(t, app)
	defer clearWebhookState(t, app)

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-events-ingress-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-events-ingress-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	clearWebhookState(t, app)

	eventSuffix := strings.TrimPrefix(session.ID, "sesn_")
	postCodeSessionIngressEvents(t, app, codeSessionID, `{"events":[
		{"type":"assistant","uuid":"assistant-`+eventSuffix+`","message":{"role":"assistant","content":"hello from worker"},"created_at":"2026-06-16T01:00:01Z"},
		{"type":"result","uuid":"result-`+eventSuffix+`","stop_reason":{"type":"end_turn"},"created_at":"2026-06-16T01:00:02Z"}
	]}`)

	events := listSessionEvents(t, app, session.ID, "order=asc", defaultTestKey)
	if !eventPageContains(events, `"type":"agent.message"`) || !eventPageContains(events, `"type":"session.status_idle"`) || !eventPageContains(events, `hello from worker`) {
		t.Fatalf("ingress events missing worker outputs: %+v", events)
	}
	if err := webhooks.RunOnce(context.Background(), app.db, app.cfg, "session-ingress-webhook-worker"); err != nil {
		t.Fatalf("deliver ingress webhook: %v", err)
	}
	mu.Lock()
	delivered := requests
	mu.Unlock()
	if delivered != 1 {
		t.Fatalf("delivered ingress webhooks = %d, want 1", delivered)
	}
	retrieved := retrieveSession(t, app, session.ID, defaultTestKey)
	if retrieved.Status != "idle" {
		t.Fatalf("session status after ingress status event = %s, want idle", retrieved.Status)
	}

	postCodeSessionIngressEvents(t, app, codeSessionID, `{"events":[
		{"type":"assistant","uuid":"assistant-`+eventSuffix+`","message":{"role":"assistant","content":"hello from worker"},"created_at":"2026-06-16T01:00:01Z"},
		{"type":"result","uuid":"result-`+eventSuffix+`","stop_reason":{"type":"end_turn"},"created_at":"2026-06-16T01:00:02Z"}
	]}`)
	again := listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey)
	if len(again.Data) != 1 {
		t.Fatalf("ingress should be idempotent, agent.message count = %d", len(again.Data))
	}
	if err := webhooks.RunOnce(context.Background(), app.db, app.cfg, "session-ingress-webhook-worker"); err != nil {
		t.Fatalf("deliver duplicate ingress webhook: %v", err)
	}
	mu.Lock()
	delivered = requests
	mu.Unlock()
	if delivered != 1 {
		t.Fatalf("delivered ingress webhooks after duplicate = %d, want 1", delivered)
	}
}

func TestSessionCanonicalMultiAgentEventsFromCodeSessionIngress(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-canonical-multi-agent-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-canonical-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-canonical-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	suffix := strings.TrimPrefix(session.ID, "sesn_")
	childThreadID := "sthr_" + suffix
	childAgentID := "agent_child_" + suffix
	orphanMCPToolUseID := "tool_orphan_mcp_" + suffix
	postCodeSessionIngressEvents(t, app, codeSessionID, `{"events":[
			{"type":"session.thread_created","uuid":"thread-created-`+suffix+`","session_thread_id":`+quoteJSON(childThreadID)+`,"agent_id":`+quoteJSON(childAgentID)+`,"agent_name":"analyst","created_at":"2026-06-16T01:00:00Z"},
			{"type":"session.thread_status_running","uuid":"thread-running-`+suffix+`","session_thread_id":`+quoteJSON(childThreadID)+`,"created_at":"2026-06-16T01:00:01Z"},
			{"type":"agent.mcp_tool_use","uuid":"orphan-mcp-tool-use-`+suffix+`","tool_use_id":`+quoteJSON(orphanMCPToolUseID)+`,"name":"mcp__weather_orphan","tool_name":"mcp__weather_orphan","evaluated_permission":"allow","input":{"location":"Beijing"},"created_at":"2026-06-16T01:00:01.500Z"},
			{"type":"agent.mcp_tool_use","uuid":"owned-mcp-tool-use-`+suffix+`","session_thread_id":`+quoteJSON(childThreadID)+`,"tool_use_id":`+quoteJSON(orphanMCPToolUseID)+`,"name":"mcp__weather_orphan","tool_name":"mcp__weather_orphan","evaluated_permission":"allow","input":{"location":"Beijing"},"created_at":"2026-06-16T01:00:01.600Z"},
			{"type":"agent.tool_result","uuid":"orphan-mcp-tool-result-`+suffix+`","tool_use_id":`+quoteJSON(orphanMCPToolUseID)+`,"content":[{"type":"text","text":"orphan weather result should not be primary"}],"created_at":"2026-06-16T01:00:01.700Z"},
			{"type":"agent.tool_use","uuid":"tool-use-`+suffix+`","session_thread_id":`+quoteJSON(childThreadID)+`,"name":"Bash","tool_name":"Bash","input":{"command":"npm test"},"created_at":"2026-06-16T01:00:02Z"},
			{"type":"agent.mcp_tool_use","uuid":"mcp-tool-use-`+suffix+`","session_thread_id":`+quoteJSON(childThreadID)+`,"name":"mcp__search","tool_name":"mcp__search","evaluated_permission":"ask","input":{"query":"release notes"},"created_at":"2026-06-16T01:00:02.500Z"},
			{"type":"agent.message","uuid":"child-message-`+suffix+`","_owner_session_thread_id":`+quoteJSON(childThreadID)+`,"content":[{"type":"text","text":"child stream answer"}],"created_at":"2026-06-16T01:00:03Z"},
			{"type":"agent.thread_message_received","uuid":"thread-received-`+suffix+`","from_session_thread_id":`+quoteJSON(childThreadID)+`,"from_agent_name":"analyst","content":[{"type":"text","text":"deliver to coordinator"}],"created_at":"2026-06-16T01:00:04Z"},
			{"type":"span.model_request_start","uuid":"span-start-`+suffix+`","_owner_session_thread_id":`+quoteJSON(childThreadID)+`,"created_at":"2026-06-16T01:00:05Z"},
		{"type":"session.thread_status_idle","uuid":"thread-idle-`+suffix+`","session_thread_id":`+quoteJSON(childThreadID)+`,"stop_reason":{"type":"end_turn"},"created_at":"2026-06-16T01:00:06Z"},
		{"type":"event_delta","uuid":"delta-`+suffix+`","delta":{"text":"stream preview only"},"created_at":"2026-06-16T01:00:07Z"}
	]}`)
	postCodeSessionIngressEvents(t, app, codeSessionID, `{"events":[
		{"type":"agent.mcp_tool_use","uuid":"agent-id-owned-mcp-tool-use-`+suffix+`","agent_id":`+quoteJSON(childAgentID)+`,"name":"mcp__agent_inferred","tool_name":"mcp__agent_inferred","evaluated_permission":"allow","input":{"location":"Shenzhen"},"created_at":"2026-06-16T01:00:08Z"}
	]}`)

	threads := listSessionThreads(t, app, session.ID, defaultTestKey)
	var child *sessionThreadAPIResponse
	for i := range threads.Data {
		if threads.Data[i].ID == childThreadID {
			child = &threads.Data[i]
			break
		}
	}
	if child == nil {
		t.Fatalf("child thread %s was not created: %+v", childThreadID, threads.Data)
	}
	if child.Status != "idle" {
		t.Fatalf("child thread status = %q, want idle", child.Status)
	}
	if child.ParentThreadID == nil || strings.TrimSpace(*child.ParentThreadID) == "" {
		t.Fatalf("child thread parent not populated: %+v", child)
	}
	if got := retrieveSession(t, app, session.ID, defaultTestKey).Status; got != "idle" {
		t.Fatalf("session status after child idle = %q, want idle", got)
	}

	primaryEvents := listSessionEvents(t, app, session.ID, "order=asc", defaultTestKey)
	for _, want := range []string{
		`"type":"session.thread_created"`,
		`"type":"session.thread_status_running"`,
		`"type":"agent.mcp_tool_use"`,
		`mcp__search`,
		`"type":"agent.thread_message_received"`,
		`deliver to coordinator`,
		`"type":"session.thread_status_idle"`,
	} {
		if !eventPageContains(primaryEvents, want) {
			t.Fatalf("primary events missing %q: %+v", want, primaryEvents.Data)
		}
	}
	for _, leaked := range []string{"npm test", "child stream answer", "span.model_request_start", "stream preview only", "mcp__weather_orphan", "mcp__agent_inferred", "orphan weather result should not be primary"} {
		if eventPageContains(primaryEvents, leaked) {
			t.Fatalf("primary stream contains child or stream-only event %q: %+v", leaked, primaryEvents.Data)
		}
	}

	childEvents := listThreadEvents(t, app, session.ID, childThreadID, defaultTestKey)
	for _, want := range []string{
		`"type":"agent.tool_use"`,
		`npm test`,
		`"type":"agent.mcp_tool_use"`,
		`mcp__search`,
		`mcp__weather_orphan`,
		`mcp__agent_inferred`,
		`"type":"agent.message"`,
		"child stream answer",
		`"type":"span.model_request_start"`,
		`"session_thread_id":"` + childThreadID + `"`,
	} {
		if !eventPageContains(childEvents, want) {
			t.Fatalf("child events missing %q: %+v", want, childEvents.Data)
		}
	}
	for _, leaked := range []string{
		"deliver to coordinator",
		"stream preview only",
		`"type":"session.thread_status_running"`,
		`"type":"session.thread_status_idle"`,
	} {
		if eventPageContains(childEvents, leaked) {
			t.Fatalf("child stream contains primary or stream-only event %q: %+v", leaked, childEvents.Data)
		}
	}
}

func TestSessionClaudeCodeTaskEventsMapToCanonicalThreads(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-claude-code-task-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-claude-code-task-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-claude-code-task-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[
		{"payload":{"type":"user","uuid":"user-task-echo","message":{"role":"user","content":"duplicate coordinator echo"},"created_at":"2026-06-16T00:59:59Z"}},
		{"payload":{"type":"assistant","uuid":"assistant-task-tool","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool_translate_zh","name":"Agent","input":{"description":"Translate to Chinese","prompt":"Translate hello, world to Chinese.","subagent_type":"general-purpose"}}]},"created_at":"2026-06-16T01:00:00Z"}},
		{"payload":{"type":"system","uuid":"system-task-started","subtype":"task_started","task_id":"taskzh123","task_type":"local_agent","tool_use_id":"tool_translate_zh","description":"Translate to Chinese","prompt":"Translate hello, world to Chinese.","created_at":"2026-06-16T01:00:01Z"}},
		{"payload":{"type":"user","uuid":"user-task-result","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_translate_zh","content":[{"type":"text","text":"你好，世界"},{"type":"text","text":"agentId: agent_123\n<usage>total_tokens: 456</usage>"}]}]},"created_at":"2026-06-16T01:00:02Z"}},
		{"payload":{"type":"system","uuid":"system-task-done","subtype":"task_notification","task_id":"taskzh123","tool_use_id":"tool_translate_zh","status":"completed","summary":"Translate to Chinese","usage":{"duration_ms":1234,"total_tokens":456},"created_at":"2026-06-16T01:00:03Z"}},
		{"payload":{"type":"result","uuid":"result-task","stop_reason":"end_turn","created_at":"2026-06-16T01:00:04Z"}}
	]}`)

	threads := listSessionThreads(t, app, session.ID, defaultTestKey)
	var child *sessionThreadAPIResponse
	for i := range threads.Data {
		if threads.Data[i].ParentThreadID != nil {
			child = &threads.Data[i]
			break
		}
	}
	if child == nil {
		events := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
		t.Fatalf("Claude Code task child thread was not created: threads=%+v events=%+v", threads.Data, events.Data)
	}
	childThreadID := child.ID
	if child.Status != "idle" {
		t.Fatalf("Claude Code task child status = %q, want idle", child.Status)
	}
	if child.ParentThreadID == nil || strings.TrimSpace(*child.ParentThreadID) == "" {
		t.Fatalf("Claude Code task child parent not populated: %+v", child)
	}

	events := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
	for _, want := range []string{
		`"type":"agent.tool_use"`,
		`"tool_name":"Agent"`,
		`Translate to Chinese`,
		`"type":"session.thread_created"`,
		`"type":"session.thread_status_running"`,
		`"type":"agent.thread_message_sent"`,
		`"type":"agent.thread_message_received"`,
		`"from_agent_name":"Translate to Chinese"`,
		`你好，世界`,
		`"type":"session.thread_status_idle"`,
		`"type":"session.status_idle"`,
		childThreadID,
	} {
		if !eventPageContains(events, want) {
			t.Fatalf("Claude Code task mapped events missing %q: %+v", want, events.Data)
		}
	}
	if eventPageContains(events, `"type":"system.message"`) {
		t.Fatalf("Claude Code task lifecycle leaked raw system.message instead of canonical events: %+v", events.Data)
	}
	if eventPageContains(events, "duplicate coordinator echo") {
		t.Fatalf("Claude Code user transcript echo leaked into public session events: %+v", events.Data)
	}
	if got := retrieveSession(t, app, session.ID, defaultTestKey).Status; got != "idle" {
		t.Fatalf("session status after Claude Code task mapping = %q, want idle", got)
	}
}

func TestSessionClaudeCodeSubagentInternalEventsPublishToChildThread(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-claude-code-subagent-internal-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-claude-code-subagent-internal-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-claude-code-subagent-internal-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[
		{"agent_id":"agent-a","payload":{"type":"user","uuid":"agent-a-private-user","agentId":"agent-a","message":{"role":"user","content":"private child prompt only in child stream"},"created_at":"2026-06-16T01:00:00Z"}},
		{"agent_id":"agent-a","payload":{"type":"assistant","uuid":"agent-a-private-thinking","agentId":"agent-a","message":{"role":"assistant","content":[{"type":"thinking","thinking":"private child thinking only in child stream"}]},"created_at":"2026-06-16T01:00:01Z"}},
		{"agent_id":"agent-a","payload":{"type":"assistant","uuid":"agent-a-private-answer","agentId":"agent-a","message":{"role":"assistant","content":[{"type":"text","text":"private child answer only in child stream"}]},"created_at":"2026-06-16T01:00:02Z"}}
	]}`)
	before := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
	for _, blocked := range []string{"private child prompt only in child stream", "private child thinking only in child stream", "private child answer only in child stream"} {
		if eventPageContains(before, blocked) {
			t.Fatalf("subagent internal event leaked before thread mapping %q: %+v", blocked, before.Data)
		}
	}

	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[
		{"payload":{"type":"assistant","uuid":"assistant-agent-tool-for-internal","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool_translate_zh","name":"Agent","input":{"description":"Translate to Chinese","prompt":"Translate hello, world to Chinese.","subagent_type":"general-purpose"}}]},"created_at":"2026-06-16T01:00:03Z"}},
		{"payload":{"type":"system","uuid":"system-agent-started-for-internal","subtype":"task_started","task_id":"agent-a","task_type":"local_agent","tool_use_id":"tool_translate_zh","description":"Translate to Chinese","prompt":"Translate hello, world to Chinese.","created_at":"2026-06-16T01:00:04Z"}}
	]}`)

	threads := listSessionThreads(t, app, session.ID, defaultTestKey)
	var child *sessionThreadAPIResponse
	for i := range threads.Data {
		if threads.Data[i].ParentThreadID != nil {
			child = &threads.Data[i]
			break
		}
	}
	if child == nil {
		t.Fatalf("child thread was not created for subagent internal transcript: %+v", threads.Data)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		delete from session_events
		where session_external_id = $1 and thread_external_id = $2
	`, session.ID, child.ID); err != nil {
		t.Fatalf("delete child session events before backfill: %v", err)
	}

	childEvents := listThreadEvents(t, app, session.ID, child.ID, defaultTestKey)
	for _, want := range []string{
		`"type":"user.message"`,
		"private child prompt only in child stream",
		`"type":"agent.thinking"`,
		"private child thinking only in child stream",
		`"type":"agent.message"`,
		"private child answer only in child stream",
		`"session_thread_id":"` + child.ID + `"`,
	} {
		if !eventPageContains(childEvents, want) {
			t.Fatalf("child transcript missing %q: %+v", want, childEvents.Data)
		}
	}
	for _, leaked := range []string{`"agentId":"agent-a"`, `"_owner_session_thread_id"`, `"type":"session.thread_status_running"`} {
		if eventPageContains(childEvents, leaked) {
			t.Fatalf("child transcript leaked internal field %q: %+v", leaked, childEvents.Data)
		}
	}

	primaryEvents := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
	for _, want := range []string{
		`"type":"agent.tool_use"`,
		`"type":"session.thread_created"`,
		`"type":"session.thread_status_running"`,
		`"type":"agent.thread_message_sent"`,
	} {
		if !eventPageContains(primaryEvents, want) {
			t.Fatalf("primary coordination missing %q: %+v", want, primaryEvents.Data)
		}
	}
	for _, blocked := range []string{"private child prompt only in child stream", "private child thinking only in child stream", "private child answer only in child stream"} {
		if eventPageContains(primaryEvents, blocked) {
			t.Fatalf("subagent internal event leaked into primary stream %q: %+v", blocked, primaryEvents.Data)
		}
	}
}

func TestSessionEventStreamReceivesCodeSessionIngressEvents(t *testing.T) {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	app := newTestAppWithStore(t, &cfg, newFakeStore("sessions-events-stream-ingress-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-events-stream-ingress-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-events-stream-ingress-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+session.ID+"/events/stream?beta=true", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	req.Header.Set("X-Api-Key", defaultTestKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	eventSuffix := strings.TrimPrefix(session.ID, "sesn_")
	postCodeSessionIngressEvents(t, app, codeSessionID, `{"events":[{"type":"assistant","uuid":"assistant-stream-`+eventSuffix+`","message":{"role":"assistant","content":"streamed from worker"},"created_at":"2026-06-16T01:10:00Z"}]}`)

	lineCh := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				t.Fatal("event stream closed before ingress event arrived")
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "agent.message") {
				return
			}
		case <-deadline:
			events := listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey)
			t.Fatalf("timed out waiting for ingress event on stream; listed local agent events=%d", len(events.Data))
		}
	}
}

func TestSessionEventStreamForwardsWorkerStreamDeltasWithoutHistory(t *testing.T) {
	ctx := context.Background()

	app := newTestAppWithStore(t, nil, newFakeStore("sessions-events-stream-delta-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-stream-delta-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-stream-delta-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+session.ID+"/events/stream?beta=true&event_deltas%5B%5D=agent.message&event_deltas%5B%5D=agent.thinking", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	req.Header.Set("X-Api-Key", defaultTestKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	lineCh := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
	}()

	suffix := strings.TrimPrefix(session.ID, "sesn_")
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[
		{"payload":{"type":"event_delta","uuid":"stream-delta-sse-`+suffix+`","delta":{"text":"stream preview over sse"},"created_at":"2026-06-16T01:10:03Z"}}
	]}`)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				t.Fatal("event stream closed before stream delta arrived")
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "stream preview over sse") {
				if !strings.Contains(line, `"type":"event_delta"`) {
					t.Fatalf("stream delta data missing event_delta type: %s", line)
				}
				events := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
				if eventPageContains(events, "stream preview over sse") {
					t.Fatalf("stream delta was persisted to session history: %+v", events.Data)
				}
				return
			}
		case <-deadline:
			events := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
			t.Fatalf("timed out waiting for stream delta on SSE; persisted events=%+v", events.Data)
		}
	}
}

func TestSessionThreadsDefaultPageSupportsOfficialPollingAndLegacyPrimary(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-threads-default-page-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-threads-default-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-threads-default-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	suffix := strings.TrimPrefix(session.ID, "sesn_")
	createdEvents := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		threadID := fmt.Sprintf("sthr_%s_%02d", suffix, i)
		createdEvents = append(createdEvents, `{"type":"session.thread_created","uuid":"thread-page-`+suffix+fmt.Sprintf("-%02d", i)+`","session_thread_id":`+quoteJSON(threadID)+`,"agent_name":"agent `+strconv.Itoa(i)+`","created_at":"2026-06-16T01:00:00Z"}`)
	}
	postCodeSessionIngressEvents(t, app, codeSessionID, `{"events":[`+strings.Join(createdEvents, ",")+`]}`)

	threads := listSessionThreads(t, app, session.ID, defaultTestKey)
	if len(threads.Data) != 26 {
		t.Fatalf("default thread page len = %d, want 26; next_page=%v data=%+v", len(threads.Data), threads.NextPage, threads.Data)
	}
	if threads.NextPage != nil {
		t.Fatalf("default thread page next_page = %q, want nil for 26 lanes", *threads.NextPage)
	}

	if _, err := app.db.Pool.Exec(context.Background(), `
		delete from session_threads
		where session_external_id = $1 and parent_thread_id is null
	`, session.ID); err != nil {
		t.Fatalf("delete primary thread: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	streamReq, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+session.ID+"/events/stream?beta=true", nil)
	if err != nil {
		cancel()
		t.Fatalf("new legacy primary stream request: %v", err)
	}
	streamReq.Header.Set("X-Api-Key", defaultTestKey)
	streamReq.Header.Set("anthropic-version", "2023-06-01")
	streamReq.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	streamReq.Header.Set("Accept", "text/event-stream")
	streamResp, err := app.client.Do(streamReq)
	if err != nil {
		cancel()
		t.Fatalf("open legacy primary stream: %v", err)
	}
	if streamResp.StatusCode != http.StatusOK {
		body := readAll(t, streamResp.Body)
		streamResp.Body.Close()
		cancel()
		t.Fatalf("legacy primary stream status = %d, want 200: %s", streamResp.StatusCode, body)
	}
	streamResp.Body.Close()
	cancel()

	legacyThreads := listSessionThreads(t, app, session.ID, defaultTestKey)
	var primaryCount int
	for _, thread := range legacyThreads.Data {
		if thread.ParentThreadID == nil {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		t.Fatalf("legacy primary repair count = %d, want 1; threads=%+v", primaryCount, legacyThreads.Data)
	}
}

func TestLegacyCodeSessionWebSocketRoutesAreRemoved(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-ws-removed-bucket"))
	defer app.close()

	for _, path := range []string{
		"/v1/session_ingress/ws/cse_retired",
		"/v2/session_ingress/ws/cse_retired",
	} {
		req, err := http.NewRequest(http.MethodGet, app.baseURL+path, nil)
		if err != nil {
			t.Fatalf("new retired websocket route request %s: %v", path, err)
		}
		resp, err := app.client.Do(req)
		if err != nil {
			t.Fatalf("request retired websocket route %s: %v", path, err)
		}
		body := readAll(t, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("retired websocket route %s status = %d, want %d: %s", path, resp.StatusCode, http.StatusNotFound, body)
		}
	}
}

func TestCodeSessionHTTPPollReceivesQueuedUserEvents(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-http-poll-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-http-poll-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-http-poll-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"queued over http poll"}]}]}`, defaultTestKey)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	req, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/code/sessions/"+codeSessionID, nil)
	if err != nil {
		t.Fatalf("new code session poll request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionID)
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("poll code session events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("poll code session events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var polled struct {
		Events []json.RawMessage `json:"events"`
	}
	decodeJSON(t, resp.Body, &polled)
	if len(polled.Events) < 2 || !eventPageContains(sessionEventPageAPIResponse{Data: polled.Events}, "queued over http poll") {
		t.Fatalf("unexpected polled events: %s", polled.Events)
	}

	// The canonical path remains an HTTP poll endpoint, but WebSocket upgrade
	// requests on it are retired. Queue an event first so a regression that
	// accidentally falls through to polling cannot wait for its 30-second empty
	// response timeout.
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"queued for upgrade-shaped poll"}]}]}`, defaultTestKey)
	upgradeReq, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/code/sessions/"+codeSessionID, nil)
	if err != nil {
		t.Fatalf("new upgrade-shaped code session poll request: %v", err)
	}
	upgradeReq.Header.Set("Authorization", "Bearer "+codeSessionID)
	upgradeReq.Header.Set("Connection", "Upgrade")
	upgradeReq.Header.Set("Upgrade", "websocket")
	upgradeResp, err := app.client.Do(upgradeReq)
	if err != nil {
		t.Fatalf("upgrade-shaped code session poll: %v", err)
	}
	defer upgradeResp.Body.Close()
	if upgradeResp.StatusCode != http.StatusNotFound {
		t.Fatalf("retired canonical websocket upgrade status = %d, want 404: %s", upgradeResp.StatusCode, readAll(t, upgradeResp.Body))
	}
}

func TestCodeSessionWorkerEndpointsPublishEvents(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	if workerEpoch != "1" {
		t.Fatalf("worker register epoch = %q, want 1", workerEpoch)
	}
	initEpoch := putCodeSessionWorker(t, app, codeSessionID, workerEpoch)
	if initEpoch != workerEpoch {
		t.Fatalf("worker init epoch = %q, want %q", initEpoch, workerEpoch)
	}
	initialReadState := getCodeSessionWorkerReadStateResponse(t, app, codeSessionID, workerEpoch)
	if metadata := codeSessionWorkerReadExternalMetadata(t, initialReadState); metadata != nil {
		t.Fatalf("initial worker read metadata = %+v, want nil", metadata)
	}

	seedState := putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"external_metadata":{"pending_action":{"tool_name":"OldTool"},"task_summary":"stale","persisted":true}}`)
	if _, ok := seedState.Worker.ExternalMetadata["persisted"]; !ok {
		t.Fatalf("seed worker metadata missing persisted key: %+v", seedState.Worker.ExternalMetadata)
	}
	initState := putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"worker_status":"idle","requires_action_details":null,"external_metadata":{"pending_action":null,"task_summary":null}}`)
	if initState.Worker.WorkerStatus != "idle" {
		t.Fatalf("init worker status = %q, want idle", initState.Worker.WorkerStatus)
	}
	if _, ok := initState.Worker.ExternalMetadata["pending_action"]; ok {
		t.Fatalf("pending_action metadata was not deleted: %+v", initState.Worker.ExternalMetadata)
	}
	if _, ok := initState.Worker.ExternalMetadata["task_summary"]; ok {
		t.Fatalf("task_summary metadata was not deleted: %+v", initState.Worker.ExternalMetadata)
	}
	if _, ok := initState.Worker.ExternalMetadata["persisted"]; !ok {
		t.Fatalf("metadata merge did not preserve persisted key: %+v", initState.Worker.ExternalMetadata)
	}

	requiresState := putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"worker_status":"requires_action","requires_action_details":{"tool_name":"Bash","action_description":"Running npm test","request_id":"req_worker_state"},"external_metadata":{"pending_action":{"tool_name":"Bash"},"persisted":{"kept":true}}}`)
	if requiresState.Worker.WorkerStatus != "requires_action" {
		t.Fatalf("requires action worker status = %q, want requires_action", requiresState.Worker.WorkerStatus)
	}
	var actionDetails map[string]string
	if err := json.Unmarshal(requiresState.Worker.RequiresActionDetails, &actionDetails); err != nil || actionDetails["tool_name"] != "Bash" {
		t.Fatalf("requires_action_details = %s, err=%v", requiresState.Worker.RequiresActionDetails, err)
	}
	var pendingAction map[string]string
	if err := json.Unmarshal(requiresState.Worker.ExternalMetadata["pending_action"], &pendingAction); err != nil || pendingAction["tool_name"] != "Bash" {
		t.Fatalf("pending_action metadata = %s, err=%v", requiresState.Worker.ExternalMetadata["pending_action"], err)
	}
	if got := retrieveSession(t, app, session.ID, defaultTestKey).Status; got != "idle" {
		t.Fatalf("public session status after requires_action = %q, want idle", got)
	}
	threads := listSessionThreads(t, app, session.ID, defaultTestKey)
	if len(threads.Data) != 1 || threads.Data[0].Status != "idle" {
		t.Fatalf("primary thread status after requires_action = %+v, want idle", threads.Data)
	}

	runningState := putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"worker_status":"running","requires_action_details":{"tool_name":"Bash"}}`)
	if runningState.Worker.WorkerStatus != "running" || !rawMessageIsJSONNull(runningState.Worker.RequiresActionDetails) {
		t.Fatalf("running worker state = %+v, details=%s; want running with cleared details", runningState.Worker, runningState.Worker.RequiresActionDetails)
	}
	if got := retrieveSession(t, app, session.ID, defaultTestKey).Status; got != "running" {
		t.Fatalf("public session status after running = %q, want running", got)
	}
	threads = listSessionThreads(t, app, session.ID, defaultTestKey)
	if len(threads.Data) != 1 || threads.Data[0].Status != "running" {
		t.Fatalf("primary thread status after running = %+v, want running", threads.Data)
	}
	runningDetailsOnlyState := putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"requires_action_details":{"tool_name":"Bash"}}`)
	if runningDetailsOnlyState.Worker.WorkerStatus != "running" || !rawMessageIsJSONNull(runningDetailsOnlyState.Worker.RequiresActionDetails) {
		t.Fatalf("running details-only worker state = %+v, details=%s; want running with cleared details", runningDetailsOnlyState.Worker, runningDetailsOnlyState.Worker.RequiresActionDetails)
	}
	if got := retrieveSession(t, app, session.ID, defaultTestKey).Status; got != "running" {
		t.Fatalf("public session status after details-only update = %q, want running", got)
	}
	threads = listSessionThreads(t, app, session.ID, defaultTestKey)
	if len(threads.Data) != 1 || threads.Data[0].Status != "running" {
		t.Fatalf("primary thread status after details-only update = %+v, want running", threads.Data)
	}

	idleState := putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"worker_status":"idle","external_metadata":{"pending_action":null}}`)
	if idleState.Worker.WorkerStatus != "idle" || !rawMessageIsJSONNull(idleState.Worker.RequiresActionDetails) {
		t.Fatalf("idle worker state = %+v, details=%s; want idle with cleared details", idleState.Worker, idleState.Worker.RequiresActionDetails)
	}
	if _, ok := idleState.Worker.ExternalMetadata["pending_action"]; ok {
		t.Fatalf("pending_action metadata was not deleted on idle: %+v", idleState.Worker.ExternalMetadata)
	}
	if got := retrieveSession(t, app, session.ID, defaultTestKey).Status; got != "idle" {
		t.Fatalf("public session status after idle = %q, want idle", got)
	}
	threads = listSessionThreads(t, app, session.ID, defaultTestKey)
	if len(threads.Data) != 1 || threads.Data[0].Status != "idle" {
		t.Fatalf("primary thread status after idle = %+v, want idle", threads.Data)
	}
	readBackState := getCodeSessionWorkerReadStateResponse(t, app, codeSessionID, workerEpoch)
	readBackMetadata := codeSessionWorkerReadExternalMetadata(t, readBackState)
	if _, ok := readBackMetadata["persisted"]; !ok {
		t.Fatalf("read back worker metadata = %+v, want persisted key", readBackMetadata)
	}
	if _, ok := readBackMetadata["pending_action"]; ok {
		t.Fatalf("read back worker metadata kept pending_action: %+v", readBackMetadata)
	}

	assertCodeSessionWorkerInternalEvents(t, app, codeSessionID)
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[{"payload":{"type":"user","uuid":"internal-`+strings.TrimPrefix(session.ID, "sesn_")+`"}}]}`)
	assertCodeSessionWorkerDelivery(t, app, codeSessionID, workerEpoch)
	assertCodeSessionWorkerHeartbeat(t, app, codeSessionID, workerEpoch)
	assertCodeSessionWorkerOTLP(t, app, codeSessionID, "metrics", workerEpoch)
	assertCodeSessionWorkerOTLP(t, app, codeSessionID, "logs", workerEpoch)

	eventSuffix := strings.TrimPrefix(session.ID, "sesn_")
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"events":[{"payload":{"type":"assistant","uuid":"assistant-worker-`+eventSuffix+`","message":{"role":"assistant","content":"hello from ccr worker"},"created_at":"2026-06-16T01:10:00Z"}}],"worker_epoch":`+quoteJSON(workerEpoch)+`}`)
	agentMessages := listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey)
	if !eventPageContains(agentMessages, "hello from ccr worker") {
		t.Fatalf("worker event was not published to session events: %+v", agentMessages.Data)
	}
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"events":[
		{"payload":{"type":"agent.mcp_tool_use","uuid":"mcp-tool-worker-`+eventSuffix+`","name":"mcp__search","tool_name":"mcp__search","created_at":"2026-06-16T01:10:01Z"}},
		{"payload":{"type":"user.tool_result","uuid":"client-tool-result-worker-`+eventSuffix+`","content":[{"type":"text","text":"client result should not be worker output"}],"created_at":"2026-06-16T01:10:02Z"}},
		{"payload":{"type":"event_delta","uuid":"stream-delta-worker-`+eventSuffix+`","delta":{"text":"stream delta should not persist"},"created_at":"2026-06-16T01:10:03Z"}}
	],"worker_epoch":`+quoteJSON(workerEpoch)+`}`)
	mcpToolEvents := listSessionEvents(t, app, session.ID, "types[]=agent.mcp_tool_use", defaultTestKey)
	if !eventPageContains(mcpToolEvents, "mcp__search") {
		t.Fatalf("canonical MCP tool event was not published to session events: %+v", mcpToolEvents.Data)
	}
	allSessionEvents := listSessionEvents(t, app, session.ID, "order=asc", defaultTestKey)
	for _, blocked := range []string{"client result should not be worker output", "stream delta should not persist"} {
		if eventPageContains(allSessionEvents, blocked) {
			t.Fatalf("blocked worker output %q leaked into public session events: %+v", blocked, allSessionEvents.Data)
		}
	}

	postCodeSessionWorkerDiagnostics(t, app, codeSessionID, `{"session_id":`+quoteJSON(codeSessionID)+`,"lines":[{"timestamp":"2026-06-16T01:11:00.000Z","fields":{"message":"diag from ccr worker"}}],"worker_epoch":`+quoteJSON(workerEpoch)+`}`)
	diagEvents := listSessionEvents(t, app, session.ID, "types[]=env_manager_log", defaultTestKey)
	if eventPageContains(diagEvents, "diag from ccr worker") {
		t.Fatalf("worker diagnostics leaked into public session events: %+v", diagEvents.Data)
	}
}

func TestSessionEventsListHidesLegacyEnvManagerLog(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-legacy-env-manager-log-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-legacy-env-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-legacy-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	ctx := context.Background()
	apiKey, err := app.db.GetAPIKey(ctx, auth.HashAPIKey(defaultTestKey))
	if err != nil {
		t.Fatalf("get api key: %v", err)
	}
	storedSession, err := app.db.GetSession(ctx, apiKey.WorkspaceID, session.ID)
	if err != nil {
		t.Fatalf("get stored session: %v", err)
	}
	eventSuffix := strings.TrimPrefix(session.ID, "sesn_")
	hiddenEventID := "sevt_legacy_env_manager_log_" + eventSuffix
	visibleEventID := "sevt_visible_after_legacy_env_" + eventSuffix
	now := time.Now().UTC()
	if _, err := app.db.AppendSessionEvents(ctx, storedSession.WorkspaceID, storedSession.ExternalID, []db.SessionEvent{
		{
			UUID:        uuid.NewString(),
			ExternalID:  hiddenEventID,
			EventType:   "env_manager_log",
			Payload:     json.RawMessage(`{"id":` + quoteJSON(hiddenEventID) + `,"type":"env_manager_log","content":"Using existing Claude Code installation (version 2.1.120)"}`),
			ProcessedAt: now,
			CreatedAt:   now,
		},
		{
			UUID:        uuid.NewString(),
			ExternalID:  visibleEventID,
			EventType:   "agent.message",
			Payload:     json.RawMessage(`{"id":` + quoteJSON(visibleEventID) + `,"type":"agent.message","content":[{"type":"text","text":"visible event after legacy env log"}]}`),
			ProcessedAt: now.Add(time.Second),
			CreatedAt:   now.Add(time.Second),
		},
	}); err != nil {
		t.Fatalf("append legacy env manager log: %v", err)
	}

	events := listSessionEvents(t, app, session.ID, "order=asc&limit=10", defaultTestKey)
	if eventPageContains(events, "Using existing Claude Code installation") {
		t.Fatalf("legacy env_manager_log leaked into public session events: %+v", events.Data)
	}
	if !eventPageContains(events, "visible event after legacy env log") {
		t.Fatalf("public event after legacy env_manager_log was hidden: %+v", events.Data)
	}
	filtered := listSessionEvents(t, app, session.ID, "types[]=env_manager_log", defaultTestKey)
	if len(filtered.Data) != 0 {
		t.Fatalf("env_manager_log type filter returned hidden events: %+v", filtered.Data)
	}
}

func TestCodeSessionWorkerInternalEventsPersistForResume(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-internal-events-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-internal-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-internal-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	beforePublicEvents := countSessionEvents(t, app, session.ID)

	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[
		{"payload":{"type":"system","uuid":"fg-system-old","content":"old system foreground"}},
		{"payload":{"type":"user","uuid":"fg-old","message":{"role":"user","content":"old foreground"}}},
		{"payload":{"type":"user","uuid":"fg-compact","message":{"role":"user","content":"compact foreground"}},"is_compaction":true},
		{"payload":{"type":"assistant","uuid":"fg-after","message":{"role":"assistant","content":"after foreground"}},"event_metadata":{"source":"resume-log","batch":1}},
		{"payload":{"type":"user","uuid":"agent-a-old","message":{"role":"user","content":"old agent a"}},"agent_id":"agent-a"},
		{"payload":{"type":"user","uuid":"agent-a-compact","message":{"role":"user","content":"compact agent a"}},"is_compaction":true,"agent_id":"agent-a"},
		{"payload":{"type":"assistant","uuid":"agent-a-after","message":{"role":"assistant","content":"after agent a"}},"agent_id":"agent-a"},
		{"payload":{"type":"attachment","uuid":"agent-b-only","agentId":"agent-b","attachment":{"type":"skill_listing","content":"only agent b"}}}
	]}`)
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[
		{"payload":{"type":"assistant","uuid":"fg-after","message":{"role":"assistant","content":"duplicate retry"}}}
	]}`)

	afterPublicEvents := countSessionEvents(t, app, session.ID)
	if afterPublicEvents != beforePublicEvents {
		t.Fatalf("internal events changed public session event count from %d to %d", beforePublicEvents, afterPublicEvents)
	}
	var duplicateCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from code_session_internal_events
		where code_session_external_id = $1 and payload_uuid = 'fg-after' and deleted_at is null
	`, codeSessionID).Scan(&duplicateCount); err != nil {
		t.Fatalf("count duplicate internal events: %v", err)
	}
	if duplicateCount != 1 {
		t.Fatalf("fg-after stored %d times, want 1", duplicateCount)
	}

	foreground := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events")
	if foreground.NextCursor != nil {
		t.Fatalf("foreground next_cursor = %q, want nil", *foreground.NextCursor)
	}
	assertInternalEventUUIDs(t, foreground.Data, []string{"fg-compact", "fg-after"})
	if !foreground.Data[0].IsCompaction || foreground.Data[0].AgentID != "" || foreground.Data[0].EventType != "user" {
		t.Fatalf("unexpected foreground compaction event: %+v", foreground.Data[0])
	}
	if foreground.Data[1].EventType != "assistant" {
		t.Fatalf("foreground event_type = %q, want assistant: %+v", foreground.Data[1].EventType, foreground.Data[1])
	}
	assertRawJSONEqual(t, foreground.Data[1].EventMetadata, `{"source":"resume-log","batch":1}`)

	subagents := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events?subagents=true")
	assertInternalEventUUIDs(t, subagents.Data, []string{"agent-a-compact", "agent-a-after", "agent-b-only"})
	if subagents.Data[0].AgentID != "agent-a" || subagents.Data[2].AgentID != "agent-b" {
		t.Fatalf("unexpected subagent IDs: %+v", subagents.Data)
	}
	if subagents.Data[2].EventType != "attachment" {
		t.Fatalf("subagent event_type = %q, want attachment: %+v", subagents.Data[2].EventType, subagents.Data[2])
	}
}

func TestCodeSessionWorkerInternalEventsRejectsStaleEpochWithoutWrite(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-internal-stale-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-internal-stale-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-internal-stale-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)
	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch1 != "1" || epoch2 != "2" {
		t.Fatalf("epochs = %q/%q, want 1/2", epoch1, epoch2)
	}

	emptyResp := doCodeSessionWorkerRequest(t, app, codeSessionID, "internal-events", `{"worker_epoch":`+quoteJSON(epoch1)+`,"events":[]}`)
	assertError(t, emptyResp, http.StatusConflict, "conflict_error")

	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "internal-events", `{"worker_epoch":`+quoteJSON(epoch1)+`,"events":[
			{"payload":{"type":"assistant","uuid":"stale-internal","message":{"role":"assistant","content":"must not persist"}}}
	]}`)
	assertError(t, resp, http.StatusConflict, "conflict_error")

	var count int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from code_session_internal_events
		where code_session_external_id = $1 and deleted_at is null
	`, codeSessionID).Scan(&count); err != nil {
		t.Fatalf("count internal events: %v", err)
	}
	if count != 0 {
		t.Fatalf("stale epoch wrote %d internal events, want 0", count)
	}
}

func TestCodeSessionWorkerInternalEventsPagination(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-internal-pagination-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-internal-pagination-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-internal-pagination-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	var body strings.Builder
	body.WriteString(`{"worker_epoch":`)
	body.WriteString(quoteJSON(workerEpoch))
	body.WriteString(`,"events":[`)
	for i := 0; i < 501; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"payload":{"type":"assistant","uuid":"page-%03d","message":{"role":"assistant","content":"event %03d"}}}`, i, i)
	}
	body.WriteString(`]}`)
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, body.String())

	first := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events")
	if len(first.Data) != 500 || first.NextCursor == nil {
		t.Fatalf("first page len=%d next=%v, want 500 with cursor", len(first.Data), first.NextCursor)
	}
	assertInternalEventUUIDs(t, first.Data[:2], []string{"page-000", "page-001"})
	second := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events?cursor="+url.QueryEscape(*first.NextCursor))
	assertInternalEventUUIDs(t, second.Data, []string{"page-500"})
	if second.NextCursor != nil {
		t.Fatalf("second page next_cursor = %q, want nil", *second.NextCursor)
	}
}

func TestCodeSessionWorkerInternalEventsValidation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-internal-validation-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-internal-validation-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-internal-validation-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	cases := []string{
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":null}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":{}}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":[]}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"user"}}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"transcript","uuid":"bad-type"}}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"progress","uuid":"bad-type"}}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"user","uuid":"bad-compaction"},"is_compaction":"true"}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"user","uuid":"bad-agent"},"agent_id":123}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"user","uuid":"conflicting-agent","agentId":"agent-a"},"agent_id":"agent-b"}]}`,
	}
	for _, body := range cases {
		resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "internal-events", body)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	}

	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, codeSessionID, "internal-events?cursor=not-a-number", "")
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

	methodResp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodPut, codeSessionID, "internal-events", "")
	defer methodResp.Body.Close()
	if methodResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("put worker internal events status = %d, want 405: %s", methodResp.StatusCode, readAll(t, methodResp.Body))
	}
}

func TestCodeSessionWorkerEventsRejectInvalidBody(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-events-validation-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-events-validation-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-events-validation-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	validPayload := `{"payload":{"type":"assistant","uuid":"valid-worker-event-` + strings.TrimPrefix(session.ID, "sesn_") + `","message":{"role":"assistant","content":"valid"}}}`

	cases := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "empty events", body: `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[]}`},
		{name: "missing worker epoch", body: `{"events":[` + validPayload + `]}`},
		{name: "invalid worker epoch", body: `{"worker_epoch":"abc","events":[` + validPayload + `]}`},
		{name: "missing payload", body: `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{}]}`},
		{name: "null payload", body: `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":null}]}`},
		{name: "payload non object", body: `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":[]}]}`},
		{name: "missing type", body: `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"uuid":"missing-type-` + strings.TrimPrefix(session.ID, "sesn_") + `"}}]}`},
		{name: "missing uuid", body: `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"assistant","message":{"role":"assistant","content":"missing uuid"}}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "events", tc.body)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		})
	}
}

func TestCodeSessionWorkerEventsAppendContract(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-events-contract-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-events-contract-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-events-contract-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	ctx := context.Background()

	beforeKeepAlive, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load before keep_alive: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[{"payload":{"type":"keep_alive"}}]}`)
	afterKeepAlive, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load after keep_alive: %v", err)
	}
	if beforeKeepAlive.LastWorkerActivityAt == nil || afterKeepAlive.LastWorkerActivityAt == nil || !afterKeepAlive.LastWorkerActivityAt.After(*beforeKeepAlive.LastWorkerActivityAt) {
		t.Fatalf("keep_alive activity before=%v after=%v, want refreshed", beforeKeepAlive.LastWorkerActivityAt, afterKeepAlive.LastWorkerActivityAt)
	}
	if got := listCodeSessionOutboundEventsForTest(t, app, codeSessionID); len(got) != 0 {
		t.Fatalf("keep_alive wrote outbound rows: %+v", got)
	}

	suffix := strings.TrimPrefix(session.ID, "sesn_")
	streamUUID := "stream-worker-" + suffix
	assistantUUID := "assistant-worker-contract-" + suffix
	batch := `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[` +
		`{"ephemeral":true,"payload":{"type":"stream_event","uuid":` + quoteJSON(streamUUID) + `,"event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ephemeral stream snapshot"}}}},` +
		`{"payload":{"type":"assistant","uuid":` + quoteJSON(assistantUUID) + `,"message":{"role":"assistant","content":"durable assistant contract"},"created_at":"2026-06-16T01:10:00Z"}}` +
		`]}`
	postCodeSessionWorkerEvents(t, app, codeSessionID, batch)
	rows := listCodeSessionOutboundEventsForTest(t, app, codeSessionID)
	if len(rows) != 2 {
		t.Fatalf("outbound rows len = %d, want 2: %+v", len(rows), rows)
	}
	if rows[0].SequenceNum != 1 || rows[0].EventType != "stream_event" || rows[0].PayloadUUID != streamUUID || !rows[0].Ephemeral {
		t.Fatalf("first outbound row = %+v, want ephemeral stream_event seq 1", rows[0])
	}
	if rows[1].SequenceNum != 2 || rows[1].EventType != "assistant" || rows[1].PayloadUUID != assistantUUID || rows[1].Ephemeral {
		t.Fatalf("second outbound row = %+v, want durable assistant seq 2", rows[1])
	}
	var streamPayload map[string]any
	if err := json.Unmarshal(rows[0].Payload, &streamPayload); err != nil {
		t.Fatalf("decode stream payload: %v", err)
	}
	if streamPayload["session_id"] != codeSessionID || strings.TrimSpace(fmt.Sprint(streamPayload["timestamp"])) == "" {
		t.Fatalf("normalized stream payload missing session_id/timestamp: %s", rows[0].Payload)
	}
	agentMessages := listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey)
	if eventPageContainsCount(agentMessages, "durable assistant contract") != 1 {
		t.Fatalf("durable assistant public projection count != 1: %+v", agentMessages.Data)
	}
	allPublicEvents := listSessionEvents(t, app, session.ID, "order=asc", defaultTestKey)
	if eventPageContains(allPublicEvents, "ephemeral stream snapshot") {
		t.Fatalf("ephemeral stream event leaked into public session events: %+v", allPublicEvents.Data)
	}

	postCodeSessionWorkerEvents(t, app, codeSessionID, batch)
	rows = listCodeSessionOutboundEventsForTest(t, app, codeSessionID)
	if len(rows) != 2 {
		t.Fatalf("duplicate batch wrote outbound rows len = %d, want 2: %+v", len(rows), rows)
	}
	agentMessages = listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey)
	if eventPageContainsCount(agentMessages, "durable assistant contract") != 1 {
		t.Fatalf("duplicate batch republished assistant event: %+v", agentMessages.Data)
	}

	controlUUID := "control-worker-" + suffix
	controlBody := `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{"type":"control_request","uuid":` + quoteJSON(controlUUID) + `,"request_id":"req_` + suffix + `","request":{"subtype":"can_use_tool","tool_use_id":"toolu_` + suffix + `","input":{"ok":true}}}}]}`
	postCodeSessionWorkerEvents(t, app, codeSessionID, controlBody)
	rows = listCodeSessionOutboundEventsForTest(t, app, codeSessionID)
	if len(rows) != 3 || rows[2].SequenceNum != 3 || rows[2].EventType != "control_request" || rows[2].PayloadUUID != controlUUID {
		t.Fatalf("control_request outbound row mismatch: %+v", rows)
	}
	var autoApproveCount int
	if err := app.db.Pool.QueryRow(ctx, `
		select count(*)
		from code_session_inbound_events
		where code_session_external_id = $1 and source = 'auto-approve' and deleted_at is null
	`, codeSessionID).Scan(&autoApproveCount); err != nil {
		t.Fatalf("count auto-approve inbound events: %v", err)
	}
	if autoApproveCount != 0 {
		t.Fatalf("control_request auto-approved %d events, want 0", autoApproveCount)
	}
	allPublicEvents = listSessionEvents(t, app, session.ID, "order=asc", defaultTestKey)
	if eventPageContains(allPublicEvents, controlUUID) {
		t.Fatalf("control_request leaked into public session events: %+v", allPublicEvents.Data)
	}
}

func TestCodeSessionMCPDefaultAllowAutoApprovesWorkerPermissionRequest(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-mcp-default-allow-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{
		"model":"claude-opus-4-6",
		"name":"sessions-worker-mcp-default-allow-agent",
		"mcp_servers":[{"type":"url","name":"weather_service","url":"http://host.docker.internal:39090/mcp"}],
		"tools":[
			{"type":"agent_toolset_20260401"},
			{
				"type":"mcp_toolset",
				"mcp_server_name":"weather_service",
				"configs":[],
				"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}
			}
		]
	}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-mcp-default-allow-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	suffix := strings.TrimPrefix(session.ID, "sesn_")
	toolUseID := "toolu_weather_" + suffix
	requestID := "req_weather_" + suffix

	controlBody := `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"events":[{"payload":{` +
		`"type":"control_request",` +
		`"uuid":"control-weather-` + suffix + `",` +
		`"request_id":` + quoteJSON(requestID) + `,` +
		`"request":{"subtype":"can_use_tool","tool_name":"mcp__weather_service__get_weather","tool_use_id":` + quoteJSON(toolUseID) + `,"input":{"location":"Beijing"}}` +
		`}}]}`
	postCodeSessionWorkerEvents(t, app, codeSessionID, controlBody)

	source, eventType, payload := latestCodeSessionInboundEventForSource(t, app, codeSessionID, "auto-approve")
	if source != "auto-approve" || eventType != "control_response" {
		t.Fatalf("auto response source/event_type = %q/%q, want auto-approve/control_response payload=%s", source, eventType, payload)
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode auto response payload: %v", err)
	}
	response := object["response"].(map[string]any)
	if response["request_id"] != requestID {
		t.Fatalf("auto response request_id = %v, want %s; payload=%s", response["request_id"], requestID, payload)
	}
	nested := response["response"].(map[string]any)
	if nested["behavior"] != "allow" || nested["toolUseID"] != toolUseID {
		t.Fatalf("auto response nested = %#v, want allow for %s; payload=%s", nested, toolUseID, payload)
	}

	allPublicEvents := listSessionEvents(t, app, session.ID, "order=asc", defaultTestKey)
	if eventPageContains(allPublicEvents, "control-weather-"+suffix) {
		t.Fatalf("control_request leaked into public session events: %+v", allPublicEvents.Data)
	}
}

func TestCodeSessionMCPDefaultAskPublishesRequiresActionAndAcceptsConfirmation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-mcp-default-ask-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{
		"model":"claude-opus-4-6",
		"name":"sessions-worker-mcp-default-ask-agent",
		"mcp_servers":[{"type":"url","name":"weather_service","url":"http://host.docker.internal:39090/mcp"}],
		"tools":[
			{"type":"agent_toolset_20260401"},
			{
				"type":"mcp_toolset",
				"mcp_server_name":"weather_service",
				"configs":[],
				"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}}
			}
		]
	}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-mcp-default-ask-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	suffix := strings.TrimPrefix(session.ID, "sesn_")
	toolUseID := "toolu_weather_ask_" + suffix
	requestID := "req_weather_ask_" + suffix

	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[{"payload":{`+
		`"type":"control_request",`+
		`"uuid":"control-weather-ask-`+suffix+`",`+
		`"request_id":`+quoteJSON(requestID)+`,`+
		`"request":{"subtype":"can_use_tool","tool_name":"mcp__weather_service__get_weather","tool_use_id":`+quoteJSON(toolUseID)+`,"input":{"location":"Beijing"}}`+
		`}}]}`)

	var autoApproveCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from code_session_inbound_events
		where code_session_external_id = $1 and source = 'auto-approve' and deleted_at is null
	`, codeSessionID).Scan(&autoApproveCount); err != nil {
		t.Fatalf("count auto-approve inbound events: %v", err)
	}
	if autoApproveCount != 0 {
		t.Fatalf("always_ask auto-approved %d events, want 0", autoApproveCount)
	}

	publicEvents := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
	for _, want := range []string{
		`"type":"agent.mcp_tool_use"`,
		`"tool_name":"mcp__weather_service__get_weather"`,
		`"evaluated_permission":"ask"`,
		`"tool_use_id":"` + toolUseID + `"`,
		`"type":"session.status_idle"`,
		`"type":"requires_action"`,
		`"request_id":"` + requestID + `"`,
	} {
		if !eventPageContains(publicEvents, want) {
			t.Fatalf("always_ask public events missing %q: %+v", want, publicEvents.Data)
		}
	}
	if eventPageContains(publicEvents, "control-weather-ask-"+suffix) {
		t.Fatalf("control_request leaked into public session events: %+v", publicEvents.Data)
	}
	toolEvent := sessionEventObjectByType(t, publicEvents, "agent.mcp_tool_use")
	toolEventID, ok := toolEvent["id"].(string)
	if !ok || toolEventID == "" {
		t.Fatalf("agent.mcp_tool_use id = %#v, want non-empty string: %#v", toolEvent["id"], toolEvent)
	}
	statusEvent := sessionEventObjectByType(t, publicEvents, "session.status_idle")
	stopReason, ok := statusEvent["stop_reason"].(map[string]any)
	if !ok {
		t.Fatalf("session.status_idle stop_reason = %#v, want object: %#v", statusEvent["stop_reason"], statusEvent)
	}
	eventIDs := stringArrayField(stopReason, "event_ids")
	if len(eventIDs) != 1 || eventIDs[0] != toolEventID {
		t.Fatalf("stop_reason.event_ids = %#v, want [%s]; status=%#v tool=%#v", eventIDs, toolEventID, statusEvent, toolEvent)
	}
	assertRequiresActionStopReasonSDKShape(t, stopReason)
	requiresActionDetails, ok := statusEvent["requires_action_details"].(map[string]any)
	if !ok {
		t.Fatalf("session.status_idle requires_action_details = %#v, want object: %#v", statusEvent["requires_action_details"], statusEvent)
	}
	if requiresActionDetails["tool_use_id"] != toolUseID || requiresActionDetails["request_id"] != requestID || requiresActionDetails["tool_name"] != "mcp__weather_service__get_weather" {
		t.Fatalf("requires_action_details = %#v, want compatibility tool_use_id/request_id/tool_name", requiresActionDetails)
	}

	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(toolEventID)+`,"result":"allow"}]}`), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send tool confirmation status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	source, eventType, payload := latestCodeSessionInboundEventForSource(t, app, codeSessionID, "tool-confirmation")
	if source != "tool-confirmation" || eventType != "control_response" {
		t.Fatalf("confirmation response source/event_type = %q/%q, want tool-confirmation/control_response payload=%s", source, eventType, payload)
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode confirmation response payload: %v", err)
	}
	response := object["response"].(map[string]any)
	if response["request_id"] != requestID {
		t.Fatalf("confirmation response request_id = %v, want %s; payload=%s", response["request_id"], requestID, payload)
	}
	nested := response["response"].(map[string]any)
	if nested["behavior"] != "allow" || nested["toolUseID"] != toolUseID {
		t.Fatalf("confirmation response nested = %#v, want allow for %s; payload=%s", nested, toolUseID, payload)
	}

	var beforeCompatCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from code_session_inbound_events
		where code_session_external_id = $1 and source = 'tool-confirmation' and deleted_at is null
	`, codeSessionID).Scan(&beforeCompatCount); err != nil {
		t.Fatalf("count tool confirmation inbound events before compat send: %v", err)
	}
	compatResp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(toolUseID)+`,"result":"allow"}]}`), defaultTestKey, true)
	defer compatResp.Body.Close()
	if compatResp.StatusCode != http.StatusOK {
		t.Fatalf("send legacy tool confirmation status = %d, want 200: %s", compatResp.StatusCode, readAll(t, compatResp.Body))
	}
	var afterCompatCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from code_session_inbound_events
		where code_session_external_id = $1 and source = 'tool-confirmation' and deleted_at is null
	`, codeSessionID).Scan(&afterCompatCount); err != nil {
		t.Fatalf("count tool confirmation inbound events after compat send: %v", err)
	}
	if afterCompatCount != beforeCompatCount+1 {
		t.Fatalf("legacy tool confirmation inbound count = %d, want %d", afterCompatCount, beforeCompatCount+1)
	}
}

func TestCodeSessionMCPDefaultAskPreservesSubagentThreadForConfirmation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-mcp-default-ask-thread-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{
		"model":"claude-opus-4-6",
		"name":"sessions-worker-mcp-default-ask-thread-agent",
		"mcp_servers":[{"type":"url","name":"weather_service","url":"http://host.docker.internal:39090/mcp"}],
		"tools":[
			{"type":"agent_toolset_20260401"},
			{
				"type":"mcp_toolset",
				"mcp_server_name":"weather_service",
				"configs":[],
				"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}}
			}
		]
	}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-mcp-default-ask-thread-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	suffix := strings.TrimPrefix(session.ID, "sesn_")
	childThreadID := "sthr_approval_" + suffix
	toolUseID := "toolu_weather_thread_" + suffix
	requestID := "req_weather_thread_" + suffix

	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[{"payload":{`+
		`"type":"session.thread_created",`+
		`"uuid":"thread-created-approval-`+suffix+`",`+
		`"session_thread_id":`+quoteJSON(childThreadID)+`,`+
		`"agent_name":"reporter",`+
		`"created_at":"2026-06-16T01:00:00Z"`+
		`}}]}`)
	threads := listSessionThreads(t, app, session.ID, defaultTestKey)
	foundChildThread := false
	for _, thread := range threads.Data {
		if thread.ID == childThreadID {
			foundChildThread = true
			break
		}
	}
	if !foundChildThread {
		t.Fatalf("child thread %s was not created: %+v", childThreadID, threads.Data)
	}

	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[{"payload":{`+
		`"type":"control_request",`+
		`"uuid":"control-weather-thread-`+suffix+`",`+
		`"session_thread_id":`+quoteJSON(childThreadID)+`,`+
		`"request_id":`+quoteJSON(requestID)+`,`+
		`"request":{"subtype":"can_use_tool","session_thread_id":`+quoteJSON(childThreadID)+`,"tool_name":"mcp__weather_service__get_weather","tool_use_id":`+quoteJSON(toolUseID)+`,"input":{"location":"Beijing"}}`+
		`}}]}`)

	primaryEvents := listSessionEvents(t, app, session.ID, "order=asc&limit=100", defaultTestKey)
	for _, want := range []string{
		`"type":"agent.mcp_tool_use"`,
		`"session_thread_id":"` + childThreadID + `"`,
		`"tool_use_id":"` + toolUseID + `"`,
		`"type":"requires_action"`,
		`"request_id":"` + requestID + `"`,
	} {
		if !eventPageContains(primaryEvents, want) {
			t.Fatalf("primary events missing %q: %+v", want, primaryEvents.Data)
		}
	}
	primaryToolEvent := sessionEventObjectByType(t, primaryEvents, "agent.mcp_tool_use")
	primaryToolEventID, ok := primaryToolEvent["id"].(string)
	if !ok || primaryToolEventID == "" {
		t.Fatalf("primary agent.mcp_tool_use id = %#v, want non-empty string: %#v", primaryToolEvent["id"], primaryToolEvent)
	}
	if primaryToolEvent["session_thread_id"] != childThreadID {
		t.Fatalf("primary blocking tool event session_thread_id = %v, want %s: %#v", primaryToolEvent["session_thread_id"], childThreadID, primaryToolEvent)
	}
	primaryStatusEvent := sessionEventObjectByType(t, primaryEvents, "session.status_idle")
	primaryStopReason, ok := primaryStatusEvent["stop_reason"].(map[string]any)
	if !ok {
		t.Fatalf("primary status stop_reason = %#v, want object: %#v", primaryStatusEvent["stop_reason"], primaryStatusEvent)
	}
	primaryEventIDs := stringArrayField(primaryStopReason, "event_ids")
	if len(primaryEventIDs) != 1 || primaryEventIDs[0] != primaryToolEventID {
		t.Fatalf("primary stop_reason.event_ids = %#v, want [%s]; status=%#v tool=%#v", primaryEventIDs, primaryToolEventID, primaryStatusEvent, primaryToolEvent)
	}
	assertRequiresActionStopReasonSDKShape(t, primaryStopReason)
	primaryRequiresActionDetails, ok := primaryStatusEvent["requires_action_details"].(map[string]any)
	if !ok {
		t.Fatalf("primary status requires_action_details = %#v, want object: %#v", primaryStatusEvent["requires_action_details"], primaryStatusEvent)
	}
	if primaryRequiresActionDetails["tool_use_id"] != toolUseID || primaryRequiresActionDetails["request_id"] != requestID || primaryRequiresActionDetails["session_thread_id"] != childThreadID {
		t.Fatalf("primary requires_action_details = %#v, want compatibility tool_use_id/request_id/session_thread_id", primaryRequiresActionDetails)
	}

	childEvents := listThreadEvents(t, app, session.ID, childThreadID, defaultTestKey)
	for _, want := range []string{
		`"type":"agent.mcp_tool_use"`,
		`"session_thread_id":"` + childThreadID + `"`,
		`"tool_use_id":"` + toolUseID + `"`,
		`"evaluated_permission":"ask"`,
	} {
		if !eventPageContains(childEvents, want) {
			t.Fatalf("child events missing %q: %+v", want, childEvents.Data)
		}
	}

	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.tool_confirmation","session_thread_id":`+quoteJSON(childThreadID)+`,"tool_use_id":`+quoteJSON(primaryToolEventID)+`,"result":"deny","deny_message":"Reporter cannot call external weather"}]}`), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send child tool confirmation status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	childEvents = listThreadEvents(t, app, session.ID, childThreadID, defaultTestKey)
	for _, want := range []string{
		`"type":"user.tool_confirmation"`,
		`"session_thread_id":"` + childThreadID + `"`,
		`"tool_use_id":"` + primaryToolEventID + `"`,
		`Reporter cannot call external weather`,
	} {
		if !eventPageContains(childEvents, want) {
			t.Fatalf("child events after confirmation missing %q: %+v", want, childEvents.Data)
		}
	}

	source, eventType, payload := latestCodeSessionInboundEventForSource(t, app, codeSessionID, "tool-confirmation")
	if source != "tool-confirmation" || eventType != "control_response" {
		t.Fatalf("confirmation response source/event_type = %q/%q, want tool-confirmation/control_response payload=%s", source, eventType, payload)
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode confirmation response payload: %v", err)
	}
	if object["session_thread_id"] != childThreadID {
		t.Fatalf("confirmation response session_thread_id = %v, want %s; payload=%s", object["session_thread_id"], childThreadID, payload)
	}
	response := object["response"].(map[string]any)
	if response["request_id"] != requestID {
		t.Fatalf("confirmation response request_id = %v, want %s; payload=%s", response["request_id"], requestID, payload)
	}
	nested := response["response"].(map[string]any)
	if nested["behavior"] != "deny" || nested["toolUseID"] != toolUseID || nested["sessionThreadID"] != childThreadID {
		t.Fatalf("confirmation response nested = %#v, want deny for %s on %s; payload=%s", nested, toolUseID, childThreadID, payload)
	}
	if nested["denyMessage"] != "Reporter cannot call external weather" {
		t.Fatalf("confirmation deny message = %v, want custom deny message; payload=%s", nested["denyMessage"], payload)
	}
}

func TestCodeSessionWorkerEventsDuplicateRetryPublishesExistingDurableEvent(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-events-duplicate-retry-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-events-duplicate-retry-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-events-duplicate-retry-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	ctx := context.Background()

	suffix := strings.TrimPrefix(session.ID, "sesn_")
	payloadUUID := "assistant-worker-duplicate-retry-" + suffix
	payload := json.RawMessage(`{"type":"assistant","uuid":` + quoteJSON(payloadUUID) + `,"session_id":` + quoteJSON(codeSessionID) + `,"created_at":"2026-06-16T01:10:00Z","timestamp":"2026-06-16T01:10:00Z","message":{"role":"assistant","content":"duplicate retry assistant"}}`)
	_, duplicate, err := app.db.AppendCodeSessionOutboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:     "csev_dup_retry_" + suffix,
		EventType:      "assistant",
		PayloadUUID:    &payloadUUID,
		Payload:        payload,
		PayloadHash:    "duplicate-retry-" + suffix,
		IdempotencyKey: codeSessionID + ":outbound:uuid:" + payloadUUID,
		Source:         "test",
	})
	if err != nil || duplicate {
		t.Fatalf("seed duplicate retry outbound event duplicate=%v err=%v", duplicate, err)
	}
	if events := listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey); eventPageContains(events, "duplicate retry assistant") {
		t.Fatalf("seeded raw outbound event unexpectedly published public event: %+v", events.Data)
	}

	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[{"payload":`+string(payload)+`,"ephemeral":true}]}`)
	rows := listCodeSessionOutboundEventsForTest(t, app, codeSessionID)
	if len(rows) != 1 {
		t.Fatalf("duplicate retry wrote outbound rows len = %d, want 1: %+v", len(rows), rows)
	}
	agentMessages := listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey)
	if eventPageContainsCount(agentMessages, "duplicate retry assistant") != 1 {
		t.Fatalf("duplicate retry did not publish one public event: %+v", agentMessages.Data)
	}

	ephemeralPayloadUUID := "assistant-worker-duplicate-retry-ephemeral-" + suffix
	ephemeralPayload := json.RawMessage(`{"type":"assistant","uuid":` + quoteJSON(ephemeralPayloadUUID) + `,"session_id":` + quoteJSON(codeSessionID) + `,"created_at":"2026-06-16T01:11:00Z","timestamp":"2026-06-16T01:11:00Z","message":{"role":"assistant","content":"ephemeral duplicate retry assistant"}}`)
	_, duplicate, err = app.db.AppendCodeSessionOutboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:     "csev_dup_retry_ephemeral_" + suffix,
		EventType:      "assistant",
		PayloadUUID:    &ephemeralPayloadUUID,
		Payload:        ephemeralPayload,
		PayloadHash:    "duplicate-retry-ephemeral-" + suffix,
		IdempotencyKey: codeSessionID + ":outbound:uuid:" + ephemeralPayloadUUID,
		Source:         "test",
		Ephemeral:      true,
	})
	if err != nil || duplicate {
		t.Fatalf("seed ephemeral duplicate retry outbound event duplicate=%v err=%v", duplicate, err)
	}
	if events := listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey); eventPageContains(events, "ephemeral duplicate retry assistant") {
		t.Fatalf("seeded ephemeral raw outbound event unexpectedly published public event: %+v", events.Data)
	}

	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"events":[{"payload":`+string(ephemeralPayload)+`,"ephemeral":false}]}`)
	rows = listCodeSessionOutboundEventsForTest(t, app, codeSessionID)
	if len(rows) != 2 || !rows[1].Ephemeral {
		t.Fatalf("ephemeral duplicate retry row mismatch: %+v", rows)
	}
	agentMessages = listSessionEvents(t, app, session.ID, "types[]=agent.message", defaultTestKey)
	if eventPageContains(agentMessages, "ephemeral duplicate retry assistant") {
		t.Fatalf("ephemeral duplicate retry published public event: %+v", agentMessages.Data)
	}
	if eventPageContainsCount(agentMessages, "duplicate retry assistant") != 1 {
		t.Fatalf("durable duplicate retry public event count changed: %+v", agentMessages.Data)
	}
}

func TestCodeSessionWorkerEventsStaleEpochDoesNotAppend(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-events-stale-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-events-stale-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-events-stale-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)
	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch1 != "1" || epoch2 != "2" {
		t.Fatalf("epochs = %q/%q, want 1/2", epoch1, epoch2)
	}
	assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, codeSessionID, "events", workerEventBody(session.ID, "stale", epoch1), http.StatusConflict, "conflict_error")
	if got := listCodeSessionOutboundEventsForTest(t, app, codeSessionID); len(got) != 0 {
		t.Fatalf("stale epoch wrote outbound rows: %+v", got)
	}
}

func TestCodeSessionWorkerRegisterEpochsAreSessionScopedAndConcurrent(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-epoch-register-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-epoch-register-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-epoch-register-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	sessionA := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionA := launchLocalCodeSession(t, app, sessionA.ID)
	sessionB := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionB := launchLocalCodeSession(t, app, sessionB.ID)

	if epoch := registerCodeSessionWorker(t, app, codeSessionA); epoch != "1" {
		t.Fatalf("session A first epoch = %q, want 1", epoch)
	}
	if epoch := registerCodeSessionWorker(t, app, codeSessionB); epoch != "1" {
		t.Fatalf("session B first epoch = %q, want 1", epoch)
	}

	type registerResult struct {
		epoch string
		err   error
	}
	results := make(chan registerResult, 2)
	for i := 0; i < 2; i++ {
		go func() {
			epoch, err := registerCodeSessionWorkerNoFatal(app, codeSessionA)
			results <- registerResult{epoch: epoch, err: err}
		}()
	}
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent register failed: %v", result.err)
		}
		seen[result.epoch] = true
	}
	if !seen["2"] || !seen["3"] || len(seen) != 2 {
		t.Fatalf("concurrent register epochs = %+v, want 2 and 3", seen)
	}
	record, err := app.db.GetCodeSession(context.Background(), codeSessionA)
	if err != nil {
		t.Fatalf("load code session A: %v", err)
	}
	if record.CurrentWorkerEpoch != 3 {
		t.Fatalf("session A current epoch = %d, want 3", record.CurrentWorkerEpoch)
	}
}

func TestCodeSessionWorkerEpochProtection(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-epoch-protection-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-epoch-protection-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-epoch-protection-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)
	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch1 != "1" || epoch2 != "2" {
		t.Fatalf("epochs = %q/%q, want 1/2", epoch1, epoch2)
	}

	assertCodeSessionWorkerWriteStatus(t, app, http.MethodPut, codeSessionID, "", workerStateBody(codeSessionID, epoch1), http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, codeSessionID, "events", workerEventBody(session.ID, "old", epoch1), http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, codeSessionID, "internal-events", `{"worker_epoch":`+quoteJSON(epoch1)+`,"events":[]}`, http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, codeSessionID, "events/delivery", workerDeliveryBody(epoch1), http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, codeSessionID, "diagnostics", workerDiagnosticsBody(codeSessionID, epoch1, "old diag"), http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, codeSessionID, "heartbeat", workerHeartbeatBody(codeSessionID, epoch1), http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerOTLPError(t, app, codeSessionID, "metrics", epoch1, http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerOTLPError(t, app, codeSessionID, "logs", epoch1, http.StatusConflict, "conflict_error")

	if got := putCodeSessionWorker(t, app, codeSessionID, epoch2); got != epoch2 {
		t.Fatalf("put current epoch response = %q, want %q", got, epoch2)
	}
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(epoch2)+`,"events":[]}`)
	assertCodeSessionWorkerDelivery(t, app, codeSessionID, epoch2)
	assertCodeSessionWorkerHeartbeat(t, app, codeSessionID, epoch2)
	assertCodeSessionWorkerOTLP(t, app, codeSessionID, "metrics", epoch2)
	assertCodeSessionWorkerOTLPJSON(t, app, codeSessionID, "metrics", epoch2)
	assertCodeSessionWorkerOTLPQueryCompatibility(t, app, codeSessionID, "metrics", epoch2)
	assertCodeSessionWorkerOTLP(t, app, codeSessionID, "logs", epoch2)
	postCodeSessionWorkerEvents(t, app, codeSessionID, workerEventBody(session.ID, "current", epoch2))
	postCodeSessionWorkerDiagnostics(t, app, codeSessionID, workerDiagnosticsBody(codeSessionID, epoch2, "current diag"))
}

func TestCodeSessionWorkerEventAppendChecksEpochInsideTransaction(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-epoch-append-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-epoch-append-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-epoch-append-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	ctx := context.Background()

	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch1 != "1" {
		t.Fatalf("first worker epoch = %q, want 1", epoch1)
	}
	if err := app.db.ValidateCodeSessionWorkerEpoch(ctx, codeSessionID, 1); err != nil {
		t.Fatalf("prevalidate epoch 1 before takeover: %v", err)
	}
	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch2 != "2" {
		t.Fatalf("second worker epoch = %q, want 2", epoch2)
	}

	staleEpoch := int64(1)
	eventSuffix := strings.TrimPrefix(codeSessionID, "cse_")
	staleID := "csev_epoch_stale_append_" + eventSuffix
	_, _, err := app.db.AppendCodeSessionOutboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:          staleID,
		EventType:           "assistant",
		Payload:             json.RawMessage(`{"type":"assistant","message":{"content":"stale write"}}`),
		PayloadHash:         "stale-write",
		Source:              "test",
		RequiredWorkerEpoch: &staleEpoch,
	})
	if !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale append error = %v, want worker epoch mismatch", err)
	}
	var staleCount int
	if err := app.db.Pool.QueryRow(ctx, `select count(*) from code_session_outbound_events where external_id = $1`, staleID).Scan(&staleCount); err != nil {
		t.Fatalf("count stale outbound events: %v", err)
	}
	if staleCount != 0 {
		t.Fatalf("stale append inserted %d events, want 0", staleCount)
	}

	currentEpoch := int64(2)
	_, _, err = app.db.AppendCodeSessionOutboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:          "csev_epoch_current_append_" + eventSuffix,
		EventType:           "assistant",
		Payload:             json.RawMessage(`{"type":"assistant","message":{"content":"current write"}}`),
		PayloadHash:         "current-write",
		Source:              "test",
		RequiredWorkerEpoch: &currentEpoch,
	})
	if err != nil {
		t.Fatalf("current epoch append: %v", err)
	}
}

func TestCodeSessionWorkerConnectionStateUpdatesAreEpochScoped(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-epoch-state-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-epoch-state-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-epoch-state-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	ctx := context.Background()

	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)
	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch1 != "1" || epoch2 != "2" {
		t.Fatalf("epochs = %q/%q, want 1/2", epoch1, epoch2)
	}
	if _, err := app.db.Pool.Exec(ctx, `
		update code_sessions
		set connection_status = 'disconnected',
			last_worker_connected_at = null,
			last_worker_activity_at = null
		where external_id = $1
	`, codeSessionID); err != nil {
		t.Fatalf("reset worker connection status: %v", err)
	}

	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, codeSessionID, "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get worker without epoch status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	afterNoEpoch, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after no-epoch get: %v", err)
	}
	if afterNoEpoch.ConnectionStatus != "disconnected" || afterNoEpoch.LastWorkerActivityAt != nil {
		t.Fatalf("no-epoch get changed worker status: %+v", afterNoEpoch)
	}

	resp = doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, codeSessionID, "?worker_epoch="+url.QueryEscape(epoch1), "")
	assertError(t, resp, http.StatusConflict, "conflict_error")
	afterStaleGet, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after stale get: %v", err)
	}
	if afterStaleGet.ConnectionStatus != "disconnected" || afterStaleGet.LastWorkerActivityAt != nil {
		t.Fatalf("stale-epoch get changed worker status: %+v", afterStaleGet)
	}

	getCodeSessionWorkerReadStateResponse(t, app, codeSessionID, epoch2)
	afterCurrentGet, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after current get: %v", err)
	}
	if afterCurrentGet.ConnectionStatus != "disconnected" || afterCurrentGet.LastWorkerActivityAt != nil || afterCurrentGet.LastWorkerConnectedAt != nil {
		t.Fatalf("current-epoch get changed worker status: %+v", afterCurrentGet)
	}

	if err := app.db.MarkCodeSessionWorkerConnectedForEpoch(ctx, codeSessionID, 1); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale epoch connect error = %v, want worker epoch mismatch", err)
	}
	afterStaleConnect, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after stale connect: %v", err)
	}
	if afterStaleConnect.ConnectionStatus != "disconnected" || afterStaleConnect.LastWorkerActivityAt != nil || afterStaleConnect.LastWorkerConnectedAt != nil {
		t.Fatalf("stale epoch connect changed worker status: %+v", afterStaleConnect)
	}
	if err := app.db.MarkCodeSessionWorkerConnectedForEpoch(ctx, codeSessionID, 2); err != nil {
		t.Fatalf("current epoch connect: %v", err)
	}
	afterCurrentConnect, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after current connect: %v", err)
	}
	if afterCurrentConnect.ConnectionStatus != "connected" || afterCurrentConnect.LastWorkerActivityAt == nil || afterCurrentConnect.LastWorkerConnectedAt == nil {
		t.Fatalf("current epoch connect did not mark worker connected: %+v", afterCurrentConnect)
	}

	if err := app.db.MarkCodeSessionWorkerDisconnectedForEpoch(ctx, codeSessionID, 1); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale epoch disconnect error = %v, want worker epoch mismatch", err)
	}
	afterStaleDisconnect, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after stale disconnect: %v", err)
	}
	if afterStaleDisconnect.ConnectionStatus != "connected" {
		t.Fatalf("stale epoch disconnect changed worker status to %q", afterStaleDisconnect.ConnectionStatus)
	}
	if err := app.db.MarkCodeSessionWorkerDisconnectedForEpoch(ctx, codeSessionID, 2); err != nil {
		t.Fatalf("current epoch disconnect: %v", err)
	}
	afterCurrentDisconnect, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after current disconnect: %v", err)
	}
	if afterCurrentDisconnect.ConnectionStatus != "disconnected" {
		t.Fatalf("current epoch disconnect status = %q, want disconnected", afterCurrentDisconnect.ConnectionStatus)
	}
}

func TestCodeSessionWorkerEpochZeroRejectedAtDBLayer(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-epoch-zero-db-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-epoch-zero-db-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-epoch-zero-db-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	ctx := context.Background()

	if err := app.db.ValidateCodeSessionWorkerEpoch(ctx, codeSessionID, 0); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("validate epoch 0 error = %v, want worker epoch mismatch", err)
	}
	if _, err := app.db.RecordCodeSessionWorkerHeartbeat(ctx, codeSessionID, 0, time.Minute, 10*time.Second); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("record heartbeat epoch 0 error = %v, want worker epoch mismatch", err)
	}
	if _, err := app.db.HeartbeatCodeSessionWorker(ctx, codeSessionID, 0, time.Minute); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("heartbeat epoch 0 error = %v, want worker epoch mismatch", err)
	}
	if err := app.db.MarkCodeSessionWorkerConnectedForEpoch(ctx, codeSessionID, 0); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("connect epoch 0 error = %v, want worker epoch mismatch", err)
	}
	if err := app.db.TouchCodeSessionWorkerActivityForEpoch(ctx, codeSessionID, 0); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("touch epoch 0 error = %v, want worker epoch mismatch", err)
	}

	zero := int64(0)
	_, _, err := app.db.AppendCodeSessionOutboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:          "csev_epoch_zero_append_" + strings.TrimPrefix(codeSessionID, "cse_"),
		EventType:           "assistant",
		Payload:             json.RawMessage(`{"type":"assistant","message":{"content":"zero write"}}`),
		PayloadHash:         "zero-write",
		Source:              "test",
		RequiredWorkerEpoch: &zero,
	})
	if !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("append epoch 0 error = %v, want worker epoch mismatch", err)
	}
}

func TestCodeSessionWorkerEpochValidationRejectsInvalidValues(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-epoch-invalid-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-epoch-invalid-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-epoch-invalid-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	cases := []string{
		`{}`,
		`[]`,
		`{`,
		`{"session_id":` + quoteJSON(codeSessionID) + `}`,
		`{"session_id":123,"worker_epoch":` + quoteJSON(workerEpoch) + `}`,
		`{"session_id":"other","worker_epoch":` + quoteJSON(workerEpoch) + `}`,
		`{"session_id":` + quoteJSON(codeSessionID) + `,"worker_epoch":"abc"}`,
		`{"session_id":` + quoteJSON(codeSessionID) + `,"worker_epoch":1.2}`,
		`{"session_id":` + quoteJSON(codeSessionID) + `,"worker_epoch":-1}`,
		`{"session_id":` + quoteJSON(codeSessionID) + `,"worker_epoch":0}`,
	}
	for _, body := range cases {
		resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "heartbeat", body)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	}
	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodPut, codeSessionID, "", `{"session_id":`+quoteJSON(codeSessionID)+`}`)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

	badStateBodies := []string{
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"worker_status":"connected"}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"worker_status":123}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"external_metadata":null}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"external_metadata":[]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"requires_action_details":[]}`,
	}
	for _, body := range badStateBodies {
		resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodPut, codeSessionID, "", body)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	}

	resp = doCodeSessionWorkerOTLPRequest(t, app, codeSessionID, "metrics", "abc", "application/x-protobuf", nil)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
}

func TestCodeSessionWorkerOTLPAcceptsMissingEpoch(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-otlp-missing-epoch-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-otlp-missing-epoch-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-otlp-missing-epoch-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	_ = registerCodeSessionWorker(t, app, codeSessionID)

	assertCodeSessionWorkerOTLP(t, app, codeSessionID, "metrics", "")
	assertCodeSessionWorkerOTLPJSON(t, app, codeSessionID, "metrics", "")
	assertCodeSessionWorkerOTLP(t, app, codeSessionID, "logs", "")
}

func TestCodeSessionWorkerOTLPFileLogWritesAcceptedTelemetry(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSessionOTLPFileLogEnabled = true
	cfg.CodeSessionOTLPLogRoot = t.TempDir()
	cfg.CodeSessionOTLPLogBodyPreviewBytes = 128
	app := newTestAppWithStore(t, &cfg, newFakeStore("sessions-code-worker-otlp-file-log-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-otlp-file-log-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-otlp-file-log-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)

	metricsBody := []byte(`{"resourceMetrics":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"claude-code"}}]},"scopeMetrics":[{"scope":{"name":"com.anthropic.claude_code"},"metrics":[{"name":"claude_code.integration.counter","sum":{"aggregationTemporality":"AGGREGATION_TEMPORALITY_CUMULATIVE","isMonotonic":true,"dataPoints":[{"timeUnixNano":"1783348800000000000","asInt":"3","attributes":[{"key":"phase","value":{"stringValue":"handler-test"}}]}]}}]}]}]}`)
	resp := doCodeSessionWorkerOTLPRequest(t, app, codeSessionID, "metrics", "", "application/json", metricsBody)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post missing-epoch metrics status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	logsBody := []byte(`{"resourceLogs":[{"scopeLogs":[{"scope":{"name":"com.anthropic.claude_code.events"},"logRecords":[{"timeUnixNano":"1783348860000000000","severityNumber":"SEVERITY_NUMBER_INFO","severityText":"INFO","body":{"stringValue":"claude_code.integration_event"},"attributes":[{"key":"event.name","value":{"stringValue":"integration_event"}}]}]}]}]}`)
	resp = doCodeSessionWorkerOTLPRequest(t, app, codeSessionID, "logs", epoch1, "application/json", logsBody)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post current-epoch logs status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	otlpDir := filepath.Join(cfg.CodeSessionOTLPLogRoot, codeSessionID, "otlp")
	requestLines := readJSONLObjectsForTest(t, filepath.Join(otlpDir, "requests.jsonl"))
	if len(requestLines) != 2 {
		t.Fatalf("request jsonl lines = %d, want 2: %#v", len(requestLines), requestLines)
	}
	firstEpoch := requestLines[0]["worker_epoch"].(map[string]any)
	if firstEpoch["present"] != false {
		t.Fatalf("missing epoch request worker_epoch = %#v, want present=false", firstEpoch)
	}
	secondEpoch := requestLines[1]["worker_epoch"].(map[string]any)
	if secondEpoch["present"] != true || secondEpoch["value"] != epoch1 {
		t.Fatalf("current epoch request worker_epoch = %#v, want epoch %s", secondEpoch, epoch1)
	}

	metricLines := readJSONLObjectsForTest(t, filepath.Join(otlpDir, "metrics.jsonl"))
	if len(metricLines) != 1 {
		t.Fatalf("metrics jsonl lines = %d, want 1: %#v", len(metricLines), metricLines)
	}
	metric := metricLines[0]["metric"].(map[string]any)
	if metric["name"] != "claude_code.integration.counter" {
		t.Fatalf("metric name = %#v, want claude_code.integration.counter", metric)
	}
	point := metricLines[0]["point"].(map[string]any)
	if point["value"].(float64) != 3 {
		t.Fatalf("metric point = %#v, want value=3", point)
	}

	logLines := readJSONLObjectsForTest(t, filepath.Join(otlpDir, "logs.jsonl"))
	if len(logLines) != 1 {
		t.Fatalf("logs jsonl lines = %d, want 1: %#v", len(logLines), logLines)
	}
	record := logLines[0]["log"].(map[string]any)
	if record["body"] != "claude_code.integration_event" {
		t.Fatalf("log record = %#v, want integration event body", record)
	}

	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch2 == epoch1 {
		t.Fatalf("epoch2 = %q, want new epoch", epoch2)
	}
	resp = doCodeSessionWorkerOTLPRequest(t, app, codeSessionID, "logs", epoch1, "application/json", logsBody)
	assertError(t, resp, http.StatusConflict, "conflict_error")
	afterConflictRequests := readJSONLObjectsForTest(t, filepath.Join(otlpDir, "requests.jsonl"))
	if len(afterConflictRequests) != len(requestLines) {
		t.Fatalf("request jsonl lines after stale epoch = %d, want %d", len(afterConflictRequests), len(requestLines))
	}
}

func TestCodeSessionWorkerHeartbeatRejectsInvalidRequests(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-heartbeat-invalid-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-heartbeat-invalid-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-heartbeat-invalid-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	resp := doCodeSessionWorkerRequestWithToken(t, app, http.MethodPost, codeSessionID, "heartbeat", workerHeartbeatBody(codeSessionID, "1"), "wrong-token")
	assertError(t, resp, http.StatusUnauthorized, "authentication_error")

	resp = doCodeSessionWorkerRequest(t, app, codeSessionID, "heartbeat", workerHeartbeatBody(codeSessionID, "1"))
	assertError(t, resp, http.StatusNotFound, "not_found_error")

	missingID := "cse_missing_heartbeat"
	resp = doCodeSessionWorkerRequest(t, app, missingID, "heartbeat", workerHeartbeatBody(missingID, "1"))
	assertError(t, resp, http.StatusNotFound, "not_found_error")
}

func TestCodeSessionWorkerHeartbeatUpdatesLeaseForCurrentEpoch(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-heartbeat-lease-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-heartbeat-lease-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-heartbeat-lease-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)
	before, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load before heartbeat: %v", err)
	}
	if before.WorkerLeaseExpiresAt == nil || before.WorkerRegisteredAt == nil {
		t.Fatalf("register did not set worker lease/register timestamps: %+v", before)
	}
	time.Sleep(5 * time.Millisecond)
	stringExpiresAt := assertCodeSessionWorkerHeartbeat(t, app, codeSessionID, epoch1)
	if !stringExpiresAt.After(*before.WorkerLeaseExpiresAt) {
		t.Fatalf("string heartbeat lease expiry = %s, want after %s", stringExpiresAt, before.WorkerLeaseExpiresAt)
	}
	time.Sleep(5 * time.Millisecond)
	numberExpiresAt := assertCodeSessionWorkerHeartbeatBody(t, app, codeSessionID, `{"session_id":`+quoteJSON(codeSessionID)+`,"worker_epoch":`+epoch1+`}`)
	if !numberExpiresAt.After(stringExpiresAt) {
		t.Fatalf("number heartbeat lease expiry = %s, want after %s", numberExpiresAt, stringExpiresAt)
	}
	after, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load after heartbeat: %v", err)
	}
	if after.WorkerLastHeartbeatAt == nil || after.WorkerLeaseExpiresAt == nil {
		t.Fatalf("heartbeat did not set heartbeat/lease timestamps: %+v", after)
	}
	if !after.WorkerLeaseExpiresAt.After(*before.WorkerLeaseExpiresAt) {
		t.Fatalf("heartbeat lease expiry = %s, want after %s", after.WorkerLeaseExpiresAt, before.WorkerLeaseExpiresAt)
	}

	beforeFutureMismatch, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load before future mismatch heartbeat: %v", err)
	}
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "heartbeat", workerHeartbeatBody(codeSessionID, "999"))
	assertError(t, resp, http.StatusConflict, "conflict_error")
	afterFutureMismatch, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load after future mismatch heartbeat: %v", err)
	}
	assertCodeSessionWorkerHeartbeatStateUnchanged(t, beforeFutureMismatch, afterFutureMismatch)

	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch2 != "2" {
		t.Fatalf("second register epoch = %q, want 2", epoch2)
	}
	beforeMismatch, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load before mismatch heartbeat: %v", err)
	}
	resp = doCodeSessionWorkerRequest(t, app, codeSessionID, "heartbeat", workerHeartbeatBody(codeSessionID, epoch1))
	assertError(t, resp, http.StatusConflict, "conflict_error")
	afterMismatch, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load after mismatch heartbeat: %v", err)
	}
	assertCodeSessionWorkerHeartbeatStateUnchanged(t, beforeMismatch, afterMismatch)

	if _, err := app.db.Pool.Exec(context.Background(), `update code_sessions set worker_lease_expires_at = now() - interval '5 seconds' where external_id = $1`, codeSessionID); err != nil {
		t.Fatalf("expire worker lease inside grace: %v", err)
	}
	graceBefore, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load grace lease record: %v", err)
	}
	assertCodeSessionWorkerHeartbeat(t, app, codeSessionID, epoch2)
	graceAfter, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load after grace heartbeat: %v", err)
	}
	if graceAfter.WorkerLeaseExpiresAt == nil || !graceAfter.WorkerLeaseExpiresAt.After(*graceBefore.WorkerLeaseExpiresAt) {
		t.Fatalf("grace heartbeat lease expiry = %v, want after %v", graceAfter.WorkerLeaseExpiresAt, graceBefore.WorkerLeaseExpiresAt)
	}

	if _, err := app.db.Pool.Exec(context.Background(), `update code_sessions set worker_lease_expires_at = now() - interval '15 seconds' where external_id = $1`, codeSessionID); err != nil {
		t.Fatalf("expire worker lease beyond grace: %v", err)
	}
	expired, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load expired lease record: %v", err)
	}
	if expired.CurrentWorkerEpoch != 2 {
		t.Fatalf("lease expiry changed epoch = %d, want 2", expired.CurrentWorkerEpoch)
	}
	resp = doCodeSessionWorkerRequest(t, app, codeSessionID, "heartbeat", workerHeartbeatBody(codeSessionID, epoch2))
	assertError(t, resp, http.StatusGone, "session_expired")
	afterExpiredHeartbeat, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("load after expired heartbeat: %v", err)
	}
	assertCodeSessionWorkerHeartbeatStateUnchanged(t, expired, afterExpiredHeartbeat)

	epoch3 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch3 != "3" {
		t.Fatalf("register after lease expiry epoch = %q, want 3", epoch3)
	}
}

func TestCodeSessionBridgeBumpsWorkerEpoch(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-bridge-epoch-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-bridge-epoch-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-bridge-epoch-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	resp := doCodeSessionBridgeRequest(t, app, codeSessionID, codeSessionID)
	assertError(t, resp, http.StatusUnauthorized, "authentication_error")

	first := bridgeCodeSessionWorker(t, app, codeSessionID)
	if first.WorkerJWT == "" || first.WorkerToken != codeSessionID || first.WorkerTokenType != "session_ingress_token" || first.APIBaseURL == "" || first.ExpiresIn <= 0 || first.WorkerEpoch != "1" {
		t.Fatalf("first bridge response = %+v, want credentials and epoch 1", first)
	}
	second := bridgeCodeSessionWorker(t, app, codeSessionID)
	if second.WorkerEpoch != "2" {
		t.Fatalf("second bridge epoch = %q, want 2", second.WorkerEpoch)
	}

	resp = doCodeSessionWorkerRequest(t, app, codeSessionID, "heartbeat", workerHeartbeatBody(codeSessionID, first.WorkerEpoch))
	assertError(t, resp, http.StatusConflict, "conflict_error")
	assertCodeSessionWorkerHeartbeat(t, app, codeSessionID, second.WorkerEpoch)
}

func TestCodeSessionWorkerEventsStreamReceivesQueuedAndLiveUserEvents(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-stream-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-stream-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-stream-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"queued over worker sse"}]}]}`, defaultTestKey)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	frames := readCodeSessionWorkerSSEFramesAfterConnect(
		t,
		app,
		codeSessionID,
		"events/stream?worker_epoch="+url.QueryEscape(workerEpoch),
		"live over worker sse",
		func() {
			sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"live over worker sse"}]}]}`, defaultTestKey)
		},
	)
	if len(frames) < 3 {
		t.Fatalf("worker SSE frames len = %d, want at least 3: %#v", len(frames), frames)
	}
	if !strings.Contains(frames[0], "event: client_event") || !strings.Contains(frames[0], `"event_type":"control_request"`) {
		t.Fatalf("unexpected first worker SSE frame: %s", frames[0])
	}
	queuedSeen := false
	for _, frame := range frames {
		if strings.Contains(frame, "queued over worker sse") {
			queuedSeen = true
			break
		}
	}
	if !queuedSeen {
		t.Fatalf("worker SSE frames missing queued event: %#v", frames)
	}
	if !strings.Contains(frames[len(frames)-1], "event: client_event") || !strings.Contains(frames[len(frames)-1], "live over worker sse") {
		t.Fatalf("unexpected user worker SSE frame: %s", frames[len(frames)-1])
	}
	frameData := decodeWorkerSSEFrameData(t, frames[len(frames)-1])
	eventID, _ := frameData["event_id"].(string)
	payload, _ := frameData["payload"].(map[string]any)
	payloadUUID, _ := payload["uuid"].(string)
	if eventID == "" || payloadUUID == "" || eventID != payloadUUID {
		t.Fatalf("worker SSE event_id = %q, payload uuid = %q, want matching payload uuid; frame=%s", eventID, payloadUUID, frames[len(frames)-1])
	}
	if strings.HasPrefix(eventID, "csev_") {
		t.Fatalf("worker SSE event_id = %q, want payload uuid not internal event id", eventID)
	}
}

func TestCodeSessionWorkerEventsStreamStopsAfterEpochTakeover(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-stream-epoch-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-stream-epoch-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-stream-epoch-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	epoch1 := registerCodeSessionWorker(t, app, codeSessionID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/code/sessions/"+codeSessionID+"/worker/events/stream?worker_epoch="+url.QueryEscape(epoch1), nil)
	if err != nil {
		t.Fatalf("new worker events stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionID)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("get worker events stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("worker events stream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	epoch2 := registerCodeSessionWorker(t, app, codeSessionID)
	if epoch2 != "2" {
		t.Fatalf("takeover epoch = %q, want 2", epoch2)
	}
	payloadUUID := "stream-takeover-" + strings.TrimPrefix(codeSessionID, "cse_")
	payload := json.RawMessage(`{"type":"user","uuid":` + quoteJSON(payloadUUID) + `,"message":{"role":"user","content":[{"type":"text","text":"queued after takeover"}]}}`)
	_, _, err = app.db.AppendCodeSessionInboundEvent(context.Background(), codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:     "csev_stream_takeover_" + strings.TrimPrefix(codeSessionID, "cse_"),
		EventType:      "user",
		PayloadUUID:    &payloadUUID,
		Payload:        payload,
		PayloadHash:    "stream-takeover",
		IdempotencyKey: "stream-takeover:" + payloadUUID,
		DeliveryStatus: "queued",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("append takeover inbound event: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(resp.Body)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("read closed stale stream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale worker stream stayed open after epoch takeover")
	}

	queued, err := app.db.ListQueuedCodeSessionInboundEvents(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("list queued inbound events after stale stream closed: %v", err)
	}
	found := false
	for _, event := range queued {
		if event.PayloadUUID != nil && *event.PayloadUUID == payloadUUID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("queued takeover event was consumed by stale stream; queued=%+v", queued)
	}
}

func TestCodeSessionWorkerEventsStreamRejectsInvalidReplayCursorWithoutConnecting(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-stream-invalid-cursor-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-code-worker-stream-invalid-cursor-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-code-worker-stream-invalid-cursor-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)
	epoch, err := strconv.ParseInt(workerEpoch, 10, 64)
	if err != nil {
		t.Fatalf("parse worker epoch: %v", err)
	}
	ctx := context.Background()
	if err := app.db.MarkCodeSessionWorkerDisconnectedForEpoch(ctx, codeSessionID, epoch); err != nil {
		t.Fatalf("mark worker disconnected before invalid stream: %v", err)
	}

	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, codeSessionID, "events/stream?worker_epoch="+url.QueryEscape(workerEpoch)+"&from_sequence_num=bad-cursor", "")
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

	after, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		t.Fatalf("load code session after invalid stream cursor: %v", err)
	}
	if after.ConnectionStatus != "disconnected" {
		t.Fatalf("invalid stream cursor changed worker status to %q, want disconnected", after.ConnectionStatus)
	}
}

func TestCodeSessionWorkerDeliveryUpdatesInboundEventStatus(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-delivery-ack-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-delivery-ack-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-delivery-ack-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	suffix := strings.TrimPrefix(codeSessionID, "cse_")
	payloadUUID := "delivery-ack-" + suffix
	externalID := "csev_delivery_ack_" + suffix
	payload := json.RawMessage(`{"type":"user","uuid":` + quoteJSON(payloadUUID) + `,"message":{"role":"user","content":[{"type":"text","text":"ack me"}]}}`)
	_, _, err := app.db.AppendCodeSessionInboundEvent(context.Background(), codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:     externalID,
		EventType:      "user",
		PayloadUUID:    &payloadUUID,
		Payload:        payload,
		PayloadHash:    "delivery-ack",
		IdempotencyKey: "delivery-ack:" + payloadUUID,
		DeliveryStatus: "queued",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("append delivery ack inbound event: %v", err)
	}
	epoch, err := strconv.ParseInt(workerEpoch, 10, 64)
	if err != nil {
		t.Fatalf("parse worker epoch: %v", err)
	}
	if err := app.db.MarkCodeSessionInboundEventSentForEpoch(context.Background(), codeSessionID, externalID, epoch); err != nil {
		t.Fatalf("mark delivery ack event sent: %v", err)
	}

	deliveryResp := postCodeSessionWorkerDelivery(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"updates":[{"event_id":`+quoteJSON(payloadUUID)+`,"status":"processing"},{"event_id":"unknown-delivery-event","status":"processed"}]}`)
	if !deliveryResp.OK || deliveryResp.Applied != 1 || deliveryResp.Ignored != 1 {
		t.Fatalf("delivery response = %+v, want ok applied=1 ignored=1", deliveryResp)
	}

	status, deliveryEpoch, receivedAt, processingAt, processedAt := loadInboundDeliveryState(t, app, externalID)
	if status != "processing" {
		t.Fatalf("delivery status after processing = %q, want processing", status)
	}
	if deliveryEpoch == nil || strconv.FormatInt(*deliveryEpoch, 10) != workerEpoch {
		t.Fatalf("delivery epoch = %v, want %s", deliveryEpoch, workerEpoch)
	}
	if receivedAt == nil || processingAt == nil || processedAt != nil {
		t.Fatalf("timestamps after processing received=%v processing=%v processed=%v, want received+processing only", receivedAt, processingAt, processedAt)
	}

	deliveryResp = postCodeSessionWorkerDelivery(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"updates":[{"event_id":`+quoteJSON(payloadUUID)+`,"status":"received"}]}`)
	if !deliveryResp.OK || deliveryResp.Applied != 1 || deliveryResp.Ignored != 0 {
		t.Fatalf("lower delivery response = %+v, want ok applied=1 ignored=0", deliveryResp)
	}
	status, _, _, _, _ = loadInboundDeliveryState(t, app, externalID)
	if status != "processing" {
		t.Fatalf("delivery status after late received = %q, want processing", status)
	}

	deliveryResp = postCodeSessionWorkerDelivery(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"updates":[{"event_id":`+quoteJSON(externalID)+`,"status":"processed"}]}`)
	if !deliveryResp.OK || deliveryResp.Applied != 1 || deliveryResp.Ignored != 0 {
		t.Fatalf("processed delivery response = %+v, want ok applied=1 ignored=0", deliveryResp)
	}
	status, _, receivedAt, processingAt, processedAt = loadInboundDeliveryState(t, app, externalID)
	if status != "processed" || receivedAt == nil || processingAt == nil || processedAt == nil {
		t.Fatalf("delivery final state status=%q received=%v processing=%v processed=%v, want processed with all timestamps", status, receivedAt, processingAt, processedAt)
	}
}

func TestCodeSessionWorkerDeliveryIgnoresUnsentOrStaleEpochEvents(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-delivery-epoch-gate-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-delivery-epoch-gate-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-delivery-epoch-gate-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	epoch1Text := registerCodeSessionWorker(t, app, codeSessionID)
	epoch1, err := strconv.ParseInt(epoch1Text, 10, 64)
	if err != nil {
		t.Fatalf("parse epoch1: %v", err)
	}

	suffix := strings.TrimPrefix(codeSessionID, "cse_")
	payloadUUID := "delivery-epoch-gate-" + suffix
	externalID := "csev_delivery_epoch_gate_" + suffix
	_, _, err = app.db.AppendCodeSessionInboundEvent(context.Background(), codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:     externalID,
		EventType:      "user",
		PayloadUUID:    &payloadUUID,
		Payload:        json.RawMessage(`{"type":"user","uuid":` + quoteJSON(payloadUUID) + `}`),
		PayloadHash:    "delivery-epoch-gate",
		IdempotencyKey: "delivery-epoch-gate:" + payloadUUID,
		DeliveryStatus: "queued",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("append epoch gate inbound event: %v", err)
	}

	deliveryResp := postCodeSessionWorkerDelivery(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(epoch1Text)+`,"updates":[{"event_id":`+quoteJSON(payloadUUID)+`,"status":"processing"}]}`)
	if !deliveryResp.OK || deliveryResp.Applied != 0 || deliveryResp.Ignored != 1 {
		t.Fatalf("queued delivery ack response = %+v, want ok applied=0 ignored=1", deliveryResp)
	}
	status, deliveryEpoch, receivedAt, processingAt, processedAt := loadInboundDeliveryState(t, app, externalID)
	if status != "queued" || deliveryEpoch != nil || receivedAt != nil || processingAt != nil || processedAt != nil {
		t.Fatalf("queued delivery state after ignored ack status=%q epoch=%v received=%v processing=%v processed=%v", status, deliveryEpoch, receivedAt, processingAt, processedAt)
	}

	if err := app.db.MarkCodeSessionInboundEventSentForEpoch(context.Background(), codeSessionID, externalID, epoch1); err != nil {
		t.Fatalf("mark epoch1 delivery sent: %v", err)
	}
	epoch2Text := registerCodeSessionWorker(t, app, codeSessionID)
	epoch2, err := strconv.ParseInt(epoch2Text, 10, 64)
	if err != nil {
		t.Fatalf("parse epoch2: %v", err)
	}
	deliveryResp = postCodeSessionWorkerDelivery(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(epoch2Text)+`,"updates":[{"event_id":`+quoteJSON(payloadUUID)+`,"status":"processed"}]}`)
	if !deliveryResp.OK || deliveryResp.Applied != 0 || deliveryResp.Ignored != 1 {
		t.Fatalf("stale-epoch delivery ack response = %+v, want ok applied=0 ignored=1", deliveryResp)
	}
	status, deliveryEpoch, _, _, processedAt = loadInboundDeliveryState(t, app, externalID)
	if status != "sent" || deliveryEpoch == nil || *deliveryEpoch != epoch1 || processedAt != nil {
		t.Fatalf("stale-epoch delivery state status=%q epoch=%v processed=%v, want sent epoch1 without processed timestamp", status, deliveryEpoch, processedAt)
	}

	if err := app.db.MarkCodeSessionInboundEventSentForEpoch(context.Background(), codeSessionID, externalID, epoch2); err != nil {
		t.Fatalf("mark epoch2 delivery sent: %v", err)
	}
	deliveryResp = postCodeSessionWorkerDelivery(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(epoch2Text)+`,"updates":[{"event_id":`+quoteJSON(payloadUUID)+`,"status":"processed"}]}`)
	if !deliveryResp.OK || deliveryResp.Applied != 1 || deliveryResp.Ignored != 0 {
		t.Fatalf("current-epoch delivery ack response = %+v, want ok applied=1 ignored=0", deliveryResp)
	}
	status, deliveryEpoch, _, _, processedAt = loadInboundDeliveryState(t, app, externalID)
	if status != "processed" || deliveryEpoch == nil || *deliveryEpoch != epoch2 || processedAt == nil {
		t.Fatalf("current-epoch delivery state status=%q epoch=%v processed=%v, want processed epoch2", status, deliveryEpoch, processedAt)
	}
}

func TestCodeSessionWorkerDeliveryRejectsInvalidUpdates(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-delivery-validation-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-delivery-validation-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-delivery-validation-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	workerEpoch := registerCodeSessionWorker(t, app, codeSessionID)

	cases := []string{
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"updates":[]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"updates":[{"event_id":"","status":"received"}]}`,
		`{"worker_epoch":` + quoteJSON(workerEpoch) + `,"updates":[{"event_id":"evt_1","status":"sent"}]}`,
	}
	for _, body := range cases {
		resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "events/delivery", body)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	}
}

func TestCodeSessionWorkerStreamReplaysUnprocessedEventsForNewEpoch(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-code-worker-stream-replay-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-worker-stream-replay-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-worker-stream-replay-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)

	epoch1Text := registerCodeSessionWorker(t, app, codeSessionID)
	epoch1, err := strconv.ParseInt(epoch1Text, 10, 64)
	if err != nil {
		t.Fatalf("parse epoch1: %v", err)
	}

	suffix := strings.TrimPrefix(codeSessionID, "cse_")
	legacyPayloadUUID := "stream-legacy-" + suffix
	legacyExternalID := "csev_stream_legacy_" + suffix
	_, _, err = app.db.AppendCodeSessionInboundEvent(context.Background(), codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:     legacyExternalID,
		EventType:      "user",
		PayloadUUID:    &legacyPayloadUUID,
		Payload:        json.RawMessage(`{"type":"user","uuid":` + quoteJSON(legacyPayloadUUID) + `}`),
		PayloadHash:    "stream-legacy",
		IdempotencyKey: "stream-legacy:" + legacyPayloadUUID,
		DeliveryStatus: "queued",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("append legacy inbound event: %v", err)
	}
	if err := app.db.MarkCodeSessionInboundEventSent(context.Background(), legacyExternalID); err != nil {
		t.Fatalf("mark legacy event sent: %v", err)
	}

	payloadUUID := "stream-replay-" + suffix
	externalID := "csev_stream_replay_" + suffix
	payload := json.RawMessage(`{"type":"user","uuid":` + quoteJSON(payloadUUID) + `,"message":{"role":"user","content":[{"type":"text","text":"replay me"}]}}`)
	event, _, err := app.db.AppendCodeSessionInboundEvent(context.Background(), codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:     externalID,
		EventType:      "user",
		PayloadUUID:    &payloadUUID,
		Payload:        payload,
		PayloadHash:    "stream-replay",
		IdempotencyKey: "stream-replay:" + payloadUUID,
		DeliveryStatus: "queued",
		Source:         "test",
	})
	if err != nil {
		t.Fatalf("append replay inbound event: %v", err)
	}
	if err := app.db.MarkCodeSessionInboundEventSentForEpoch(context.Background(), codeSessionID, externalID, epoch1); err != nil {
		t.Fatalf("mark replay event sent: %v", err)
	}

	epoch2Text := registerCodeSessionWorker(t, app, codeSessionID)
	epoch2, err := strconv.ParseInt(epoch2Text, 10, 64)
	if err != nil {
		t.Fatalf("parse epoch2: %v", err)
	}

	frames := readCodeSessionWorkerSSEFramesFromSuffix(t, app, codeSessionID, "events/stream?worker_epoch="+url.QueryEscape(epoch2Text), "replay me")
	for _, frame := range frames {
		if strings.Contains(frame, legacyPayloadUUID) {
			t.Fatalf("new epoch stream included legacy sent/null epoch event %q: frames=%+v", legacyPayloadUUID, frames)
		}
	}
	frameData := decodeWorkerSSEFrameData(t, frames[len(frames)-1])
	eventID, _ := frameData["event_id"].(string)
	payloadData, _ := frameData["payload"].(map[string]any)
	replayedPayloadUUID, _ := payloadData["uuid"].(string)
	if eventID != payloadUUID || replayedPayloadUUID != payloadUUID {
		t.Fatalf("replay stream event_id=%q payload uuid=%q, want %q; frame=%s", eventID, replayedPayloadUUID, payloadUUID, frames[len(frames)-1])
	}
	waitInboundDeliveryStatusForEpoch(t, app, externalID, "sent", epoch2)

	afterReplay, err := app.db.ListCodeSessionInboundEventsForWorkerStream(context.Background(), codeSessionID, epoch2, event.SequenceNum)
	if err != nil {
		t.Fatalf("list replay events after sequence: %v", err)
	}
	if codeSessionEventsContainPayloadUUID(afterReplay, payloadUUID) {
		t.Fatalf("after-sequence replay included already seen event %q: %+v", payloadUUID, afterReplay)
	}

	deliveryResp := postCodeSessionWorkerDelivery(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(epoch2Text)+`,"updates":[{"event_id":`+quoteJSON(payloadUUID)+`,"status":"processed"}]}`)
	if !deliveryResp.OK || deliveryResp.Applied != 1 || deliveryResp.Ignored != 0 {
		t.Fatalf("processed replay delivery response = %+v, want ok applied=1 ignored=0", deliveryResp)
	}
	replay, err := app.db.ListCodeSessionInboundEventsForWorkerStream(context.Background(), codeSessionID, epoch2, 0)
	if err != nil {
		t.Fatalf("list replay events after processed: %v", err)
	}
	if codeSessionEventsContainPayloadUUID(replay, payloadUUID) {
		t.Fatalf("processed event replayed: %+v", replay)
	}
}

func TestSessionEventInputValidation(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-events-validation-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-events-validation-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-events-validation-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)

	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.message","content":[]}]}`), defaultTestKey, true)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

	resp = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.tool_confirmation","result":"allow"}]}`), defaultTestKey, true)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

	resp = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+session.ID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.define_outcome","description":"done"}]}`), defaultTestKey, true)
	assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

	valid := sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.custom_tool_result","custom_tool_use_id":"ctool_123"},{"type":"user.define_outcome","description":"done","rubric":{"type":"text","text":"must pass"},"max_iterations":2}]}`, defaultTestKey)
	if len(valid.Data) != 2 || !bytes.Contains(valid.Data[0], []byte(`"type":"user.custom_tool_result"`)) || !bytes.Contains(valid.Data[1], []byte(`"type":"user.define_outcome"`)) {
		t.Fatalf("unexpected valid events response: %+v", valid)
	}
}

func TestSessionWebhooks(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []capturedWebhookRequest
	)
	failFirst := true
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read webhook body: %v", err)
		}
		mu.Lock()
		requests = append(requests, capturedWebhookRequest{Header: r.Header.Clone(), Body: append([]byte(nil), body...)})
		shouldFail := failFirst
		failFirst = false
		mu.Unlock()
		if shouldFail {
			http.Error(w, "retry", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	signingKey := "whsec_c2VjcmV0Cg=="
	cfg.WebhookEndpointURL = receiver.URL
	cfg.WebhookSigningKey = signingKey
	cfg.WebhookEventTypes = []string{"session.created"}
	cfg.WebhookWorkerEnabled = true
	cfg.WebhookAllowInsecure = true
	cfg.WebhookTimeout = time.Second
	cfg.WebhookMaxAttempts = 3
	app := newTestAppWithStore(t, &cfg, newFakeStore("sessions-webhooks-bucket"))
	defer app.close()
	clearWebhookState(t, app)
	defer clearWebhookState(t, app)

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"sessions-webhook-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	env := createEnvironment(t, app, `{"name":"sessions-webhook-env"}`)
	defer cleanupEnvironmentRows(t, app.db, env.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)

	if count := webhookJobCount(t, app, "session.created", session.ID); count != 1 {
		t.Fatalf("session.created webhook jobs = %d, want 1", count)
	}
	if count := webhookJobCount(t, app, "session.pending", session.ID); count != 0 {
		t.Fatalf("session.pending webhook jobs = %d, want 0 due filter", count)
	}

	if err := webhooks.RunOnce(context.Background(), app.db, app.cfg, "webhook-test-worker"); err != nil {
		t.Fatalf("webhook run once failure path: %v", err)
	}
	status, attempts := latestWebhookJobStatus(t, app, "session.created", session.ID)
	if status != "retry" || attempts != 1 {
		t.Fatalf("after 500 status=%s attempts=%d, want retry/1", status, attempts)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `update jobs set run_after = now() where type = 'webhook_delivery' and payload->>'event_type' = 'session.created' and payload->'event'->'data'->>'id' = $1`, session.ID); err != nil {
		t.Fatalf("reset webhook run_after: %v", err)
	}
	if err := webhooks.RunOnce(context.Background(), app.db, app.cfg, "webhook-test-worker"); err != nil {
		t.Fatalf("webhook run once success path: %v", err)
	}
	status, attempts = latestWebhookJobStatus(t, app, "session.created", session.ID)
	if status != "completed" || attempts != 1 {
		t.Fatalf("after 2xx status=%s attempts=%d, want completed/1", status, attempts)
	}

	mu.Lock()
	if len(requests) != 2 {
		t.Fatalf("webhook receiver saw %d requests, want 2", len(requests))
	}
	delivered := requests[1]
	mu.Unlock()
	client := anthropic.NewClient(option.WithWebhookKey(signingKey), option.WithAPIKey(defaultTestKey))
	event, err := client.Beta.Webhooks.Unwrap(delivered.Body, delivered.Header)
	if err != nil {
		t.Fatalf("SDK failed to unwrap webhook: %v", err)
	}
	var deliveredPayload struct {
		Type string `json:"type"`
		Data struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(delivered.Body, &deliveredPayload); err != nil {
		t.Fatalf("unmarshal delivered webhook: %v", err)
	}
	if event.Type != "event" || deliveredPayload.Type != "event" || deliveredPayload.Data.Type != "session.created" || deliveredPayload.Data.ID != session.ID {
		t.Fatalf("unexpected webhook event=%+v payload=%+v body=%s", event, deliveredPayload, delivered.Body)
	}
}

func TestSessionsSchemaHasNoForeignKeys(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("sessions-schema-bucket"))
	defer app.close()

	var foreignKeyCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where con.contype = 'f'
			and ns.oid = current_schema()::regnamespace
			and cls.relname in (
				'sessions', 'session_threads', 'session_events', 'session_resources',
				'code_sessions', 'code_session_inbound_events', 'code_session_outbound_events',
				'code_session_internal_events'
			)
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("count sessions foreign keys: %v", err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("sessions foreign key count = %d, want 0", foreignKeyCount)
	}
}

type capturedWebhookRequest struct {
	Header http.Header
	Body   []byte
}

func doSessionRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new session request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do session request: %v", err)
	}
	return resp
}

func doSessionBearerRequest(t *testing.T, app *testApp, method, path string, body io.Reader, token string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new bearer session request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do bearer session request: %v", err)
	}
	return resp
}

func createSession(t *testing.T, app *testApp, body string) sessionAPIResponse {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create session status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var session sessionAPIResponse
	decodeJSON(t, resp.Body, &session)
	if session.ID == "" {
		t.Fatalf("create session returned empty id: %+v", session)
	}
	return session
}

func retrieveSession(t *testing.T, app *testApp, sessionID, key string) sessionAPIResponse {
	t.Helper()
	var resp *http.Response
	if strings.HasPrefix(key, "sk-ant-env-") {
		resp = doSessionBearerRequest(t, app, http.MethodGet, "/v1/sessions/"+sessionID+"?beta=true", nil, key, true)
	} else {
		resp = doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+sessionID+"?beta=true", nil, key, true)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve session status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var session sessionAPIResponse
	decodeJSON(t, resp.Body, &session)
	return session
}

func updateSession(t *testing.T, app *testApp, sessionID, body string) sessionAPIResponse {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update session status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var session sessionAPIResponse
	decodeJSON(t, resp.Body, &session)
	return session
}

func archiveSession(t *testing.T, app *testApp, sessionID string) sessionAPIResponse {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"/archive?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive session status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var session sessionAPIResponse
	decodeJSON(t, resp.Body, &session)
	return session
}

func deleteSession(t *testing.T, app *testApp, sessionID string) {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodDelete, "/v1/sessions/"+sessionID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete session status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func listSessions(t *testing.T, app *testApp, query string) sessionPageAPIResponse {
	t.Helper()
	path := "/v1/sessions?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doSessionRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list sessions status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page sessionPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func sendSessionEvents(t *testing.T, app *testApp, sessionID, body, key string) sessionEventSendAPIResponse {
	t.Helper()
	var resp *http.Response
	if strings.HasPrefix(key, "sk-ant-env-") {
		resp = doSessionBearerRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"/events?beta=true", strings.NewReader(body), key, true)
	} else {
		resp = doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"/events?beta=true", strings.NewReader(body), key, true)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send session events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var events sessionEventSendAPIResponse
	decodeJSON(t, resp.Body, &events)
	return events
}

func listSessionEvents(t *testing.T, app *testApp, sessionID, query, key string) sessionEventPageAPIResponse {
	t.Helper()
	path := "/v1/sessions/" + sessionID + "/events?beta=true"
	if query != "" {
		path += "&" + query
	}
	var resp *http.Response
	if strings.HasPrefix(key, "sk-ant-env-") {
		resp = doSessionBearerRequest(t, app, http.MethodGet, path, nil, key, true)
	} else {
		resp = doSessionRequest(t, app, http.MethodGet, path, nil, key, true)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list session events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page sessionEventPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func listThreadEvents(t *testing.T, app *testApp, sessionID, threadID, key string) sessionEventPageAPIResponse {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+sessionID+"/threads/"+threadID+"/events?beta=true", nil, key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list thread events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page sessionEventPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func sessionEventStringField(t *testing.T, raw json.RawMessage, field string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode session event: %v; raw=%s", err, raw)
	}
	value, ok := payload[field].(string)
	if !ok || value == "" {
		t.Fatalf("session event %s = %#v, want non-empty string; raw=%s", field, payload[field], raw)
	}
	return value
}

func eventPageContains(events sessionEventPageAPIResponse, needle string) bool {
	for _, event := range events.Data {
		if bytes.Contains(event, []byte(needle)) {
			return true
		}
	}
	return false
}

func sessionEventObjectByType(t *testing.T, events sessionEventPageAPIResponse, eventType string) map[string]any {
	t.Helper()
	for _, raw := range events.Data {
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatalf("decode session event %s: %v", raw, err)
		}
		if object["type"] == eventType {
			return object
		}
	}
	t.Fatalf("session event type %q not found in %+v", eventType, events.Data)
	return nil
}

func stringArrayField(object map[string]any, field string) []string {
	items, _ := object[field].([]any)
	values := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}

func assertRequiresActionStopReasonSDKShape(t *testing.T, stopReason map[string]any) {
	t.Helper()
	if stopReason["type"] != "requires_action" {
		t.Fatalf("stop_reason.type = %v, want requires_action: %#v", stopReason["type"], stopReason)
	}
	for _, field := range []string{"tool_name", "tool_use_id", "request_id", "session_thread_id"} {
		if _, ok := stopReason[field]; ok {
			t.Fatalf("stop_reason contains compatibility field %q, want SDK shape only: %#v", field, stopReason)
		}
	}
}

func eventPageContainsCount(events sessionEventPageAPIResponse, needle string) int {
	count := 0
	for _, event := range events.Data {
		if bytes.Contains(event, []byte(needle)) {
			count++
		}
	}
	return count
}

type codeSessionOutboundEventForTest struct {
	SequenceNum int64
	EventType   string
	PayloadUUID string
	Payload     json.RawMessage
	Ephemeral   bool
}

func listCodeSessionOutboundEventsForTest(t *testing.T, app *testApp, codeSessionID string) []codeSessionOutboundEventForTest {
	t.Helper()
	rows, err := app.db.Pool.Query(context.Background(), `
		select sequence_num, event_type, coalesce(payload_uuid, ''), payload, ephemeral
		from code_session_outbound_events
		where code_session_external_id = $1 and deleted_at is null
		order by sequence_num asc
	`, codeSessionID)
	if err != nil {
		t.Fatalf("list code session outbound events: %v", err)
	}
	defer rows.Close()
	events := []codeSessionOutboundEventForTest{}
	for rows.Next() {
		var event codeSessionOutboundEventForTest
		var payload []byte
		if err := rows.Scan(&event.SequenceNum, &event.EventType, &event.PayloadUUID, &payload, &event.Ephemeral); err != nil {
			t.Fatalf("scan code session outbound event: %v", err)
		}
		event.Payload = json.RawMessage(append([]byte(nil), payload...))
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate code session outbound events: %v", err)
	}
	return events
}

func latestCodeSessionInboundEventForSource(t *testing.T, app *testApp, codeSessionID string, source string) (string, string, json.RawMessage) {
	t.Helper()
	var gotSource string
	var eventType string
	var payload []byte
	if err := app.db.Pool.QueryRow(context.Background(), `
		select source, event_type, payload
		from code_session_inbound_events
		where code_session_external_id = $1 and source = $2 and deleted_at is null
		order by sequence_num desc
		limit 1
	`, codeSessionID, source).Scan(&gotSource, &eventType, &payload); err != nil {
		t.Fatalf("load latest inbound event source=%s: %v", source, err)
	}
	return gotSource, eventType, json.RawMessage(append([]byte(nil), payload...))
}

func launchLocalCodeSession(t *testing.T, app *testApp, sessionID string) string {
	t.Helper()
	ctx := context.Background()
	cfg := app.cfg
	provider := &recordingRunnerProvider{sandboxID: "sandbox-" + strings.TrimPrefix(sessionID, "sesn_")}
	runner := environments.NewRunnerWithConfig(app.db, provider, cfg)
	deadline := time.Now().Add(10 * time.Second)
	for {
		processed, err := runner.RunOnce(ctx, "sessions-code-session-test")
		if err != nil {
			t.Fatalf("run environment runner for code session: %v", err)
		}
		retrieved := retrieveSession(t, app, sessionID, defaultTestKey)
		codeSessionID, runtime := codeSessionMetadata(retrieved.Metadata)
		if codeSessionID != "" {
			if runtime != "claude_code_local" {
				t.Fatalf("code session runtime = %q, want claude_code_local; metadata=%s", runtime, retrieved.Metadata)
			}
			return codeSessionID
		}
		if !processed && time.Now().After(deadline) {
			t.Fatalf("runner did not create local code session before deadline; metadata=%s", retrieved.Metadata)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func codeSessionMetadata(raw json.RawMessage) (string, string) {
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return "", ""
	}
	codeSessionID, _ := metadata["claude_code_session_id"].(string)
	runtime, _ := metadata["runtime"].(string)
	return strings.TrimSpace(codeSessionID), strings.TrimSpace(runtime)
}

func postCodeSessionIngressEvents(t *testing.T, app *testApp, codeSessionID string, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/v2/session_ingress/session/"+codeSessionID+"/events", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new ingress request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("post ingress events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post ingress events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func registerCodeSessionWorker(t *testing.T, app *testApp, codeSessionID string) string {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "register", `{"session_id":`+quoteJSON(codeSessionID)+`}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register worker status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var response struct {
		WorkerEpoch string `json:"worker_epoch"`
	}
	decodeJSON(t, resp.Body, &response)
	return response.WorkerEpoch
}

func registerCodeSessionWorkerNoFatal(app *testApp, codeSessionID string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/code/sessions/"+codeSessionID+"/worker/register", strings.NewReader(`{"session_id":`+quoteJSON(codeSessionID)+`}`))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	var response struct {
		WorkerEpoch string `json:"worker_epoch"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.WorkerEpoch) == "" {
		return "", fmt.Errorf("empty worker_epoch in response: %s", body)
	}
	return response.WorkerEpoch, nil
}

type bridgeWorkerResponse struct {
	WorkerJWT       string `json:"worker_jwt"`
	WorkerToken     string `json:"worker_token"`
	WorkerTokenType string `json:"worker_token_type"`
	APIBaseURL      string `json:"api_base_url"`
	ExpiresIn       int    `json:"expires_in"`
	WorkerEpoch     string `json:"worker_epoch"`
}

type codeSessionWorkerStateAPIResponse struct {
	OK          bool   `json:"ok"`
	SessionID   string `json:"session_id"`
	Status      string `json:"status"`
	WorkerEpoch string `json:"worker_epoch"`
	Worker      struct {
		ExternalMetadata      map[string]json.RawMessage `json:"external_metadata"`
		WorkerEpoch           string                     `json:"worker_epoch"`
		WorkerStatus          string                     `json:"worker_status"`
		RequiresActionDetails json.RawMessage            `json:"requires_action_details"`
	} `json:"worker"`
}

type codeSessionWorkerReadAPIResponse struct {
	Worker map[string]json.RawMessage `json:"worker"`
}

func bridgeCodeSessionWorker(t *testing.T, app *testApp, codeSessionID string) bridgeWorkerResponse {
	t.Helper()
	resp := doCodeSessionBridgeRequest(t, app, codeSessionID, defaultTestKey)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bridge worker status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var response bridgeWorkerResponse
	decodeJSON(t, resp.Body, &response)
	return response
}

func doCodeSessionBridgeRequest(t *testing.T, app *testApp, codeSessionID string, bearerToken string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/code/sessions/"+codeSessionID+"/bridge", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new code session bridge request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do code session bridge request: %v", err)
	}
	return resp
}

func putCodeSessionWorker(t *testing.T, app *testApp, codeSessionID string, workerEpoch string) string {
	t.Helper()
	response := putCodeSessionWorkerState(t, app, codeSessionID, workerStateBody(codeSessionID, workerEpoch))
	return response.WorkerEpoch
}

func putCodeSessionWorkerState(t *testing.T, app *testApp, codeSessionID string, body string) codeSessionWorkerStateAPIResponse {
	t.Helper()
	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodPut, codeSessionID, "", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put worker status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var response codeSessionWorkerStateAPIResponse
	decodeJSON(t, resp.Body, &response)
	return response
}

func getCodeSessionWorkerReadStateResponse(t *testing.T, app *testApp, codeSessionID string, workerEpoch string) codeSessionWorkerReadAPIResponse {
	t.Helper()
	suffix := ""
	if workerEpoch != "" {
		suffix = "?worker_epoch=" + url.QueryEscape(workerEpoch)
	}
	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, codeSessionID, suffix, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get worker status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	body := readAll(t, resp.Body)
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(body, &topLevel); err != nil {
		t.Fatalf("decode get worker response %s: %v", body, err)
	}
	if len(topLevel) != 1 {
		t.Fatalf("get worker top-level keys = %+v, want only worker", topLevel)
	}
	rawWorker, ok := topLevel["worker"]
	if !ok {
		t.Fatalf("get worker response missing worker: %s", body)
	}
	var worker map[string]json.RawMessage
	if err := json.Unmarshal(rawWorker, &worker); err != nil || worker == nil {
		t.Fatalf("decode get worker worker object %s: %v", rawWorker, err)
	}
	for key := range worker {
		if key != "external_metadata" {
			t.Fatalf("get worker key %q present in worker object; want only external_metadata", key)
		}
	}
	return codeSessionWorkerReadAPIResponse{Worker: worker}
}

func codeSessionWorkerReadExternalMetadata(t *testing.T, response codeSessionWorkerReadAPIResponse) map[string]json.RawMessage {
	t.Helper()
	rawMetadata, ok := response.Worker["external_metadata"]
	if !ok {
		return nil
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(rawMetadata, &metadata); err != nil || metadata == nil {
		t.Fatalf("decode worker external_metadata %s: %v", rawMetadata, err)
	}
	return metadata
}

func rawMessageIsJSONNull(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return bytes.Equal(raw, []byte("null"))
}

type codeSessionInternalEventsPage struct {
	Data       []codeSessionInternalEventItem `json:"data"`
	NextCursor *string                        `json:"next_cursor"`
}

type codeSessionInternalEventItem struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload"`
	EventMetadata json.RawMessage `json:"event_metadata"`
	IsCompaction  bool            `json:"is_compaction"`
	CreatedAt     string          `json:"created_at"`
	AgentID       string          `json:"agent_id"`
}

func getCodeSessionWorkerInternalEvents(t *testing.T, app *testApp, codeSessionID string, suffix string) codeSessionInternalEventsPage {
	t.Helper()
	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, codeSessionID, suffix, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get worker internal events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page codeSessionInternalEventsPage
	decodeJSON(t, resp.Body, &page)
	for _, event := range page.Data {
		if event.EventID == "" || event.EventType == "" || event.CreatedAt == "" {
			t.Fatalf("incomplete internal event response: %+v", event)
		}
	}
	return page
}

func assertInternalEventUUIDs(t *testing.T, events []codeSessionInternalEventItem, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("internal event count = %d, want %d: %+v", len(events), len(want), events)
	}
	for i, event := range events {
		var payload struct {
			UUID string `json:"uuid"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("decode internal event payload %d: %v", i, err)
		}
		if payload.UUID != want[i] {
			t.Fatalf("internal event %d uuid = %q, want %q; events=%+v", i, payload.UUID, want[i], events)
		}
	}
}

func assertRawJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode got json %q: %v", string(got), err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode want json %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("json = %s, want %s", string(got), want)
	}
}

func countSessionEvents(t *testing.T, app *testApp, sessionID string) int {
	t.Helper()
	var count int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from session_events
		where session_external_id = $1 and deleted_at is null
	`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count session events: %v", err)
	}
	return count
}

func assertCodeSessionWorkerInternalEvents(t *testing.T, app *testApp, codeSessionID string) {
	t.Helper()
	resp := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, codeSessionID, "internal-events", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get worker internal events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var response struct {
		InternalEvents []json.RawMessage `json:"internal_events"`
		Data           []json.RawMessage `json:"data"`
		HasMore        bool              `json:"has_more"`
	}
	decodeJSON(t, resp.Body, &response)
	if len(response.InternalEvents) != 0 || len(response.Data) != 0 || response.HasMore {
		t.Fatalf("unexpected worker internal events response: %+v", response)
	}
}

func postCodeSessionWorkerInternalEvents(t *testing.T, app *testApp, codeSessionID string, body string) {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "internal-events", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker internal events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func assertCodeSessionWorkerDelivery(t *testing.T, app *testApp, codeSessionID string, workerEpoch string) {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "events/delivery", `{"worker_epoch":`+quoteJSON(workerEpoch)+`,"updates":[{"event_id":"csev_delivery_test","status":"processed"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker events delivery status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func loadInboundDeliveryState(t *testing.T, app *testApp, eventExternalID string) (string, *int64, *time.Time, *time.Time, *time.Time) {
	t.Helper()
	var status string
	var deliveryEpoch *int64
	var receivedAt *time.Time
	var processingAt *time.Time
	var processedAt *time.Time
	if err := app.db.Pool.QueryRow(context.Background(), `
		select delivery_status, delivery_worker_epoch, received_at, processing_at, processed_at
		from code_session_inbound_events
		where external_id = $1 and deleted_at is null
	`, eventExternalID).Scan(&status, &deliveryEpoch, &receivedAt, &processingAt, &processedAt); err != nil {
		t.Fatalf("load inbound delivery state event_id=%s: %v", eventExternalID, err)
	}
	return status, deliveryEpoch, receivedAt, processingAt, processedAt
}

type codeSessionWorkerDeliveryAPIResponse struct {
	OK      bool `json:"ok"`
	Applied int  `json:"applied"`
	Ignored int  `json:"ignored"`
}

func postCodeSessionWorkerDelivery(t *testing.T, app *testApp, codeSessionID string, body string) codeSessionWorkerDeliveryAPIResponse {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "events/delivery", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker delivery status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var deliveryResp codeSessionWorkerDeliveryAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&deliveryResp); err != nil {
		t.Fatalf("decode delivery response: %v", err)
	}
	return deliveryResp
}

func waitInboundDeliveryStatusForEpoch(t *testing.T, app *testApp, eventExternalID string, wantStatus string, wantEpoch int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, deliveryEpoch, _, _, _ := loadInboundDeliveryState(t, app, eventExternalID)
		if status == wantStatus && deliveryEpoch != nil && *deliveryEpoch == wantEpoch {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery state for %s status=%q epoch=%v, want status=%q epoch=%d", eventExternalID, status, deliveryEpoch, wantStatus, wantEpoch)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func codeSessionEventsContainPayloadUUID(events []db.CodeSessionEvent, payloadUUID string) bool {
	for _, event := range events {
		if event.PayloadUUID != nil && *event.PayloadUUID == payloadUUID {
			return true
		}
	}
	return false
}

func assertCodeSessionWorkerHeartbeat(t *testing.T, app *testApp, codeSessionID string, workerEpoch string) time.Time {
	t.Helper()
	return assertCodeSessionWorkerHeartbeatBody(t, app, codeSessionID, `{"session_id":`+quoteJSON(codeSessionID)+`,"worker_epoch":`+quoteJSON(workerEpoch)+`}`)
}

func assertCodeSessionWorkerHeartbeatBody(t *testing.T, app *testApp, codeSessionID string, body string) time.Time {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "heartbeat", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker heartbeat status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var response struct {
		OK                   bool   `json:"ok"`
		WorkerLeaseExpiresAt string `json:"worker_lease_expires_at"`
	}
	decodeJSON(t, resp.Body, &response)
	if !response.OK {
		t.Fatalf("post worker heartbeat ok = false, want true")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, response.WorkerLeaseExpiresAt)
	if err != nil {
		t.Fatalf("worker_lease_expires_at = %q, want RFC3339Nano: %v", response.WorkerLeaseExpiresAt, err)
	}
	return expiresAt
}

func assertCodeSessionWorkerHeartbeatStateUnchanged(t *testing.T, before db.CodeSession, after db.CodeSession) {
	t.Helper()
	if !nullableTimeEqual(before.WorkerLeaseExpiresAt, after.WorkerLeaseExpiresAt) {
		t.Fatalf("heartbeat changed worker lease expiry from %v to %v", before.WorkerLeaseExpiresAt, after.WorkerLeaseExpiresAt)
	}
	if !nullableTimeEqual(before.WorkerLastHeartbeatAt, after.WorkerLastHeartbeatAt) {
		t.Fatalf("heartbeat changed worker last heartbeat from %v to %v", before.WorkerLastHeartbeatAt, after.WorkerLastHeartbeatAt)
	}
	if !nullableTimeEqual(before.LastWorkerActivityAt, after.LastWorkerActivityAt) {
		t.Fatalf("heartbeat changed last worker activity from %v to %v", before.LastWorkerActivityAt, after.LastWorkerActivityAt)
	}
}

func nullableTimeEqual(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Equal(*right)
}

func postCodeSessionWorkerEvents(t *testing.T, app *testApp, codeSessionID string, body string) {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "events", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker events status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func postCodeSessionWorkerDiagnostics(t *testing.T, app *testApp, codeSessionID string, body string) {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "diagnostics", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker diagnostics status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func assertCodeSessionWorkerOTLP(t *testing.T, app *testApp, codeSessionID string, suffix string, workerEpoch string) {
	t.Helper()
	resp := doCodeSessionWorkerOTLPRequest(t, app, codeSessionID, suffix, workerEpoch, "application/x-protobuf", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker otlp/%s status = %d, want 200: %s", suffix, resp.StatusCode, readAll(t, resp.Body))
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/x-protobuf") {
		t.Fatalf("post worker otlp/%s content-type = %q, want application/x-protobuf", suffix, contentType)
	}
	if body := readAll(t, resp.Body); len(body) != 0 {
		t.Fatalf("post worker otlp/%s body = %q, want empty protobuf response", suffix, string(body))
	}
}

func assertCodeSessionWorkerOTLPJSON(t *testing.T, app *testApp, codeSessionID string, suffix string, workerEpoch string) {
	t.Helper()
	resp := doCodeSessionWorkerOTLPRequest(t, app, codeSessionID, suffix, workerEpoch, "application/json", []byte(`{"resourceMetrics":[]}`))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker otlp/%s json status = %d, want 200: %s", suffix, resp.StatusCode, readAll(t, resp.Body))
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("post worker otlp/%s json content-type = %q, want application/json", suffix, contentType)
	}
	if body := strings.TrimSpace(string(readAll(t, resp.Body))); body != "{}" {
		t.Fatalf("post worker otlp/%s json body = %q, want {}", suffix, body)
	}
}

func assertCodeSessionWorkerOTLPQueryCompatibility(t *testing.T, app *testApp, codeSessionID string, suffix string, workerEpoch string) {
	t.Helper()
	resp := doCodeSessionWorkerRequest(t, app, codeSessionID, "otlp/"+suffix+"?worker_epoch="+url.QueryEscape(workerEpoch), `{"resourceMetrics":[]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post worker otlp/%s query epoch status = %d, want 200: %s", suffix, resp.StatusCode, readAll(t, resp.Body))
	}
	if body := strings.TrimSpace(string(readAll(t, resp.Body))); body != "{}" {
		t.Fatalf("post worker otlp/%s query epoch body = %q, want {}", suffix, body)
	}
}

func assertCodeSessionWorkerOTLPError(t *testing.T, app *testApp, codeSessionID string, suffix string, workerEpoch string, status int, errorType string) {
	t.Helper()
	resp := doCodeSessionWorkerOTLPRequest(t, app, codeSessionID, suffix, workerEpoch, "application/x-protobuf", nil)
	assertError(t, resp, status, errorType)
}

func assertCodeSessionWorkerWriteStatus(t *testing.T, app *testApp, method string, codeSessionID string, suffix string, body string, status int, errorType string) {
	t.Helper()
	resp := doCodeSessionWorkerRequestWithMethod(t, app, method, codeSessionID, suffix, body)
	if status >= http.StatusBadRequest {
		assertError(t, resp, status, errorType)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("worker write status = %d, want %d: %s", resp.StatusCode, status, readAll(t, resp.Body))
	}
}

func workerStateBody(codeSessionID string, workerEpoch string) string {
	return `{"session_id":` + quoteJSON(codeSessionID) + `,"worker_epoch":` + quoteJSON(workerEpoch) + `}`
}

func workerHeartbeatBody(codeSessionID string, workerEpoch string) string {
	return `{"session_id":` + quoteJSON(codeSessionID) + `,"worker_epoch":` + quoteJSON(workerEpoch) + `}`
}

func workerDeliveryBody(workerEpoch string) string {
	return `{"worker_epoch":` + quoteJSON(workerEpoch) + `,"updates":[{"event_id":"csev_delivery_test","status":"processed"}]}`
}

func workerEventBody(sessionID string, label string, workerEpoch string) string {
	eventSuffix := strings.TrimPrefix(sessionID, "sesn_")
	return `{"events":[{"payload":{"type":"assistant","uuid":"assistant-worker-` + label + `-` + eventSuffix + `","message":{"role":"assistant","content":"hello from ` + label + ` epoch"},"created_at":"2026-06-16T01:10:00Z"}}],"worker_epoch":` + quoteJSON(workerEpoch) + `}`
}

func workerDiagnosticsBody(codeSessionID string, workerEpoch string, message string) string {
	return `{"session_id":` + quoteJSON(codeSessionID) + `,"lines":[{"timestamp":"2026-06-16T01:11:00.000Z","fields":{"message":` + quoteJSON(message) + `}}],"worker_epoch":` + quoteJSON(workerEpoch) + `}`
}

func doCodeSessionWorkerRequest(t *testing.T, app *testApp, codeSessionID string, suffix string, body string) *http.Response {
	t.Helper()
	return doCodeSessionWorkerRequestWithMethod(t, app, http.MethodPost, codeSessionID, suffix, body)
}

func doCodeSessionWorkerRequestWithMethod(t *testing.T, app *testApp, method string, codeSessionID string, suffix string, body string) *http.Response {
	t.Helper()
	return doCodeSessionWorkerRequestWithToken(t, app, method, codeSessionID, suffix, body, codeSessionID)
}

func doCodeSessionWorkerRequestWithToken(t *testing.T, app *testApp, method string, codeSessionID string, suffix string, body string, token string) *http.Response {
	t.Helper()
	path := app.baseURL + "/v1/code/sessions/" + codeSessionID + "/worker"
	if strings.TrimSpace(suffix) != "" {
		if strings.HasPrefix(suffix, "?") {
			path += suffix
		} else {
			path += "/" + suffix
		}
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, path, reader)
	if err != nil {
		t.Fatalf("new code session worker request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do code session worker request: %v", err)
	}
	return resp
}

func doCodeSessionWorkerOTLPRequest(t *testing.T, app *testApp, codeSessionID string, suffix string, workerEpoch string, contentType string, body []byte) *http.Response {
	t.Helper()
	path := app.baseURL + "/v1/code/sessions/" + codeSessionID + "/worker/otlp/" + strings.TrimPrefix(suffix, "/")
	req, err := http.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new code session worker otlp request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionID)
	req.Header.Set("Content-Type", contentType)
	if strings.TrimSpace(workerEpoch) != "" {
		req.Header.Set("X-Worker-Epoch", workerEpoch)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do code session worker otlp request: %v", err)
	}
	return resp
}

func readJSONLObjectsForTest(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsonl %s: %v", path, err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	result := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		var object map[string]any
		if err := json.Unmarshal(line, &object); err != nil {
			t.Fatalf("decode jsonl line %q: %v", string(line), err)
		}
		result = append(result, object)
	}
	return result
}

func readCodeSessionWorkerSSEFramesFromSuffix(t *testing.T, app *testApp, codeSessionID string, suffix string, waitFor string) []string {
	t.Helper()
	return readCodeSessionWorkerSSEFramesAfterConnect(t, app, codeSessionID, suffix, waitFor, nil)
}

func readCodeSessionWorkerSSEFramesAfterConnect(t *testing.T, app *testApp, codeSessionID string, suffix string, waitFor string, afterConnect func()) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/code/sessions/"+codeSessionID+"/worker/"+strings.TrimPrefix(suffix, "/"), nil)
	if err != nil {
		t.Fatalf("new worker events stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+codeSessionID)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("get worker events stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("worker events stream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	if afterConnect != nil {
		afterConnect()
	}
	reader := bufio.NewReader(resp.Body)
	frames := []string{}
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read worker events stream: %v; frames=%#v", err, frames)
		}
		if line == "\n" || line == "\r\n" {
			raw := frame.String()
			frame.Reset()
			if strings.TrimSpace(raw) == "" || strings.HasPrefix(raw, ":") {
				continue
			}
			frames = append(frames, raw)
			if strings.Contains(raw, waitFor) {
				return frames
			}
			continue
		}
		frame.WriteString(line)
	}
}

func decodeWorkerSSEFrameData(t *testing.T, frame string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(frame, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var data map[string]any
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				t.Fatalf("decode worker SSE frame data: %v; frame=%s", err, frame)
			}
			return data
		}
	}
	t.Fatalf("worker SSE frame has no data line: %s", frame)
	return nil
}

func listSessionThreads(t *testing.T, app *testApp, sessionID, key string) sessionThreadPageAPIResponse {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+sessionID+"/threads?beta=true", nil, key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list session threads status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page sessionThreadPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func retrieveSessionThread(t *testing.T, app *testApp, sessionID, threadID, key string) sessionThreadAPIResponse {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+sessionID+"/threads/"+threadID+"?beta=true", nil, key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve session thread status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var thread sessionThreadAPIResponse
	decodeJSON(t, resp.Body, &thread)
	return thread
}

func archiveSessionThread(t *testing.T, app *testApp, sessionID, threadID string) sessionThreadAPIResponse {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"/threads/"+threadID+"/archive?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive session thread status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var thread sessionThreadAPIResponse
	decodeJSON(t, resp.Body, &thread)
	return thread
}

func containsSession(sessions []sessionAPIResponse, id string) bool {
	for _, session := range sessions {
		if session.ID == id {
			return true
		}
	}
	return false
}

func createEnvironmentKeyForTest(t *testing.T, app *testApp, envID, key string) string {
	t.Helper()
	record, err := app.db.GetEnvironment(context.Background(), getDefaultDBIDs(t, app.db).WorkspaceID, envID)
	if err != nil {
		t.Fatalf("get environment for key: %v", err)
	}
	if err := app.db.CreateEnvironmentKey(context.Background(), db.EnvironmentKey{
		ExternalID:            "envkey_" + strings.ReplaceAll(envID, "-", "_"),
		OrganizationID:        record.OrganizationID,
		WorkspaceID:           record.WorkspaceID,
		EnvironmentID:         record.ID,
		EnvironmentExternalID: record.ExternalID,
	}, auth.HashAPIKey(key)); err != nil {
		t.Fatalf("create environment key: %v", err)
	}
	return key
}

func sessionWorkData(t *testing.T, app *testApp, sessionID string) (string, string, string) {
	t.Helper()
	var workType, workSessionID, state string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select data->>'type', data->>'id', state
		from environment_work
		where data->>'id' = $1 and deleted_at is null
		order by created_at desc
		limit 1
	`, sessionID).Scan(&workType, &workSessionID, &state); err != nil {
		t.Fatalf("load session work: %v", err)
	}
	return workType, workSessionID, state
}

func webhookJobCount(t *testing.T, app *testApp, eventType, resourceID string) int {
	t.Helper()
	var count int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from jobs
		where type = 'webhook_delivery'
			and payload->>'event_type' = $1
			and payload->'event'->'data'->>'id' = $2
	`, eventType, resourceID).Scan(&count); err != nil {
		t.Fatalf("count webhook jobs: %v", err)
	}
	return count
}

func latestWebhookJobStatus(t *testing.T, app *testApp, eventType, resourceID string) (string, int) {
	t.Helper()
	var status string
	var attempts int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select status, attempts
		from jobs
		where type = 'webhook_delivery'
			and payload->>'event_type' = $1
			and payload->'event'->'data'->>'id' = $2
		order by created_at desc, id desc
		limit 1
	`, eventType, resourceID).Scan(&status, &attempts); err != nil {
		t.Fatalf("load webhook job status: %v", err)
	}
	return status, attempts
}
