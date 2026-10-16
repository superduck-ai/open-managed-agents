package tests

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestThinkingStatusStartsBeforeAnswerAndDeduplicatesWorkerEcho(t *testing.T) {
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_status\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"private reasoning\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"answer\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()
	defer unblock()
	app := newPayloadIntegrationApp(t, newFakeStore("thinking-status"))
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Thinking status", upstream.URL, "provider-key", messagesTestModel)
	agent := createAgent(t, app, `{"model":"`+messagesTestModel+`","name":"thinking-status"}`)
	environment := createEnvironment(t, app, `{"name":"thinking-status"}`)
	createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
	epoch := registerCodeSessionWorker(t, app, credential.CodeSessionID)
	code, found, err := app.db.GetCodeSession(t.Context(), credential.CodeSessionID)
	if err != nil || !found {
		t.Fatalf("code session: %v", err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(`{"model":"`+messagesTestModel+`","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", credential.Token)
	started := make(chan string, 1)
	finished := make(chan error, 1)
	go func() {
		response, err := app.client.Do(request)
		if err != nil {
			finished <- err
			return
		}
		defer response.Body.Close()
		started <- response.Header.Get("Request-Id")
		_, err = io.Copy(io.Discard, response.Body)
		finished <- err
	}()
	var requestID string
	select {
	case requestID = <-started:
	case err := <-finished:
		t.Fatalf("request failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("thinking start was not forwarded")
	}
	thinkingID := maevents.StableAssistantEventID(code.ExternalID, "msg_status", 0, "agent.thinking")
	thinking, err := app.db.GetSessionEvent(t.Context(), code.WorkspaceUUID, code.SessionExternalID, thinkingID)
	if err != nil || thinking.EventType != "agent.thinking" || strings.Contains(string(thinking.Payload), "content") {
		t.Fatalf("thinking not persisted before answer: %+v %v", thinking, err)
	}
	echo := func(index int) {
		postCodeSessionWorkerEvents(t, app, code.ExternalID, fmt.Sprintf(`{"worker_epoch":%q,"events":[{"payload":{"type":"assistant","uuid":"status-echo-%d","request_id":%q,"content_block_index":0,"message":{"id":"msg_status","content":[{"type":"thinking","thinking":"worker private reasoning","signature":"private signature"}]}}}]}`, epoch, index, requestID))
	}
	echo(1)
	unblock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("response did not finish")
	}
	_ = requestLifecycleEvents(t, app, code, 2)
	echo(2)
	history := listSessionEvents(t, app, code.SessionExternalID, "order=asc&limit=100", defaultTestKey)
	var types []string
	var thinkingCount int
	for _, raw := range history.Data {
		kind := sessionEventStringField(t, raw, "type")
		if kind == "agent.thinking" || kind == "agent.message" || kind == "span.model_request_end" {
			types = append(types, kind)
		}
		if kind == "agent.thinking" {
			thinkingCount++
		}
		if strings.Contains(string(raw), "private reasoning") || strings.Contains(string(raw), "private signature") {
			t.Fatalf("public thinking leaked: %s", raw)
		}
	}
	if strings.Join(types, ",") != "agent.thinking,agent.message,span.model_request_end" || thinkingCount != 1 {
		t.Fatalf("public order=%v thinking count=%d", types, thinkingCount)
	}
	after, err := app.db.GetSessionEvent(t.Context(), code.WorkspaceUUID, code.SessionExternalID, thinkingID)
	if err != nil || !after.ProcessedAt.Equal(thinking.ProcessedAt) {
		t.Fatalf("echo changed thinking start: %v", err)
	}
}
