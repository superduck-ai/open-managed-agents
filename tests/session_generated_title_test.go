//go:build integration

package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestSessionGeneratedTitle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	titleRequests := make(chan string, 4)
	releaseModel := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			System   string `json:"system"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil ||
			r.URL.Path != "/v1/messages" || request.System == "" ||
			len(request.Messages) != 1 || request.Messages[0].Role != "user" {
			t.Error("invalid title request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		titleRequests <- request.Messages[0].Content
		select {
		case <-releaseModel:
			fmt.Fprint(w, `{"stop_reason":"end_turn","content":[{"type":"text","text":"排查会话延迟"}]}`)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	app := newTestAppWithStore(t, nil, newFakeStore("session-title"))
	defer app.close()
	app.client.Timeout = 5 * time.Second
	provider := seedTestLLMProvider(t, app, "Session title", upstream.URL, "title-test-key", "title-test-model")
	model := llmproviders.QualifiedModelID(provider.Slug, "title-test-model")
	agent := createAgent(t, app, `{"name":"title-agent","model":`+quoteJSON(model)+`}`)
	environment := createEnvironment(t, app, `{"name":"title-environment"}`)
	sessionBody := `{"agent":` + quoteJSON(agent.ID) + `,"environment_id":` + quoteJSON(environment.ID)
	workers := river.NewWorkers()
	client, err := riverjobs.NewClient(app.db, nil, workers, map[string]river.QueueConfig{sessions.TitleQueue: {MaxWorkers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	app.server.Config.Handler.(*api.Server).RegisterSessionTitleWorker(workers, client)
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := client.StopAndCancel(stopCtx); err != nil {
			t.Error(err)
		}
	}()

	message := `{"type":"user.message","content":[{"type":"text","text":"帮我排查会话延迟"}]}`
	named := createSession(t, app, sessionBody+`,"title":"手动标题","initial_events":[`+message+`]}`)
	if named.Title == nil || *named.Title != "手动标题" {
		t.Fatalf("expected explicit title, got %v", named.Title)
	}
	listedJobs, err := client.JobList(ctx, river.NewJobListParams().Kinds(sessions.TitleQueue))
	if err != nil || len(listedJobs.Jobs) != 0 {
		t.Fatalf("explicitly named session queued a title: %v, %v", listedJobs, err)
	}

	session := createSession(t, app, sessionBody+`}`)
	if session.Title != nil {
		t.Fatal("new session already has a title")
	}
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	seedTitleTestChildMessage(t, app, codeSessionID, session.ID)
	stream := openGeneratedTitleStream(t, ctx, app, session.ID)
	defer stream.Close()
	sendSessionEvents(t, app, session.ID, `{"events":[`+message+`]}`, defaultTestKey)
	select {
	case firstPrompt := <-titleRequests:
		if firstPrompt != "帮我排查会话延迟" {
			t.Fatalf("unexpected title input %q", firstPrompt)
		}
	case <-ctx.Done():
		t.Fatal("title worker did not call the model")
	}
	if pending := retrieveSession(t, app, session.ID, defaultTestKey); pending.Title != nil {
		t.Fatal("title was set before the model completed")
	}
	if count := countQueuedCodeSessionInboundEvents(app, codeSessionID, "user", "帮我排查会话延迟"); count != 1 {
		t.Fatalf("first input appeared %d times in the Agent queue, want one", count)
	}
	sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"Later message"}]}]}`, defaultTestKey)
	close(releaseModel)

	expectGeneratedTitleEvent(t, stream)
	got := retrieveSession(t, app, session.ID, defaultTestKey)
	if got.Title == nil || *got.Title != "排查会话延迟" || got.Status != "running" {
		t.Fatalf("unexpected session after title generation: %+v", got)
	}
	history := listSessionEvents(t, app, session.ID, "", defaultTestKey)
	var updates int
	for _, event := range history.Data {
		var item struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(event, &item); err != nil {
			t.Fatal(err)
		}
		switch item.Type {
		case "session.updated":
			updates++
		case "agent.message", "span.model_request_end":
			t.Fatalf("title request leaked into Agent history as %s", item.Type)
		}
	}
	if updates != 1 {
		t.Fatalf("got %d session updates, want one", updates)
	}
	listedJobs, err = client.JobList(ctx, river.NewJobListParams().Kinds(sessions.TitleQueue))
	if err != nil || len(listedJobs.Jobs) != 1 {
		t.Fatalf("first and later inputs queued unexpected title jobs: %v, %v", listedJobs, err)
	}

	removeFailure := installWebhookMutationFailure(t, app, "river_job", "INSERT", "NEW.kind = 'session_title'")
	defer removeFailure()
	unnamed := createSession(t, app, sessionBody+`}`)
	workerID := launchLocalCodeSession(t, app, unnamed.ID)
	sendSessionEvents(t, app, unnamed.ID, `{"events":[`+message+`]}`, defaultTestKey)
	if count := countQueuedCodeSessionInboundEvents(app, workerID, "user", "帮我排查会话延迟"); count != 1 {
		t.Fatalf("title queue failure prevented input delivery: got %d queued inputs, want one", count)
	}
}

func openGeneratedTitleStream(t *testing.T, ctx context.Context, app *testApp, sessionID string) io.ReadCloser {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, app.baseURL+"/v1/sessions/"+sessionID+"/events/stream?beta=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Api-Key", defaultTestKey)
	request.Header.Set("Anthropic-Version", "2023-06-01")
	request.Header.Set("Anthropic-Beta", "managed-agents-2026-04-01")
	request.Header.Set("Accept", "text/event-stream")
	response, err := app.server.Client().Transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		t.Fatalf("unexpected stream response %d", response.StatusCode)
	}
	return response.Body
}

func expectGeneratedTitleEvent(t *testing.T, stream io.Reader) {
	t.Helper()
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		payload, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var frame struct {
			Type  string `json:"type"`
			Title string `json:"title"`
		}
		if json.Unmarshal([]byte(payload), &frame) == nil && frame.Type == "session.updated" {
			if frame.Title != "排查会话延迟" {
				t.Fatalf("unexpected SSE title %q", frame.Title)
			}
			return
		}
	}
	t.Fatalf("stream ended before the title was delivered: %v", scanner.Err())
}

func seedTitleTestChildMessage(t *testing.T, app *testApp, codeSessionID, sessionID string) {
	t.Helper()
	ctx := t.Context()
	code, found, err := app.db.GetCodeSession(ctx, codeSessionID)
	if err != nil || !found {
		t.Fatalf("load test code session: found=%t, %v", found, err)
	}
	primary, found, err := app.db.GetPrimarySessionThread(ctx, code.WorkspaceUUID, sessionID)
	if err != nil || !found {
		t.Fatalf("load primary thread: found=%t, %v", found, err)
	}
	child := primary
	child.UUID, child.ExternalID = uuid.NewV4().String(), "thread_"+uuid.NewV4().String()
	child.ParentThreadUUID, child.ParentThreadExternalID = new(primary.UUID), new(primary.ExternalID)
	if _, err := app.db.CreateSessionThreadIfAbsent(ctx, child); err != nil {
		t.Fatal(err)
	}
	changes, err := app.db.AppendSessionEventsIfAbsent(ctx, code.WorkspaceUUID, sessionID, []db.SessionEvent{{
		UUID: uuid.NewV4().String(), ExternalID: "sevt_" + uuid.NewV4().String(),
		EventType: "user.message", ThreadExternalID: &child.ExternalID, CreatedAt: time.Now(),
		Payload: []byte(`{"type":"user.message","content":[{"type":"text","text":"Child task prompt"}]}`),
	}})
	if err != nil || len(changes.Events) != 1 {
		t.Fatalf("persist child thread history: %d events, %v", len(changes.Events), err)
	}
}
