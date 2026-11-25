//go:build integration

package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/devinit"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestCloudHarnessText(t *testing.T) {
	baseURL, worker, providers := os.Getenv("LIVE_WORKER_API_URL"), os.Getenv("LIVE_HARNESS_WORKER"), os.Getenv("LIVE_HARNESS_PROVIDERS")
	if baseURL == "" || worker == "" || providers == "" || os.Getenv("CONFIG_FILE") == "" {
		t.Skip("requires an isolated API, Harness worker executable, and real provider configuration")
	}
	cfg, err := config.Load()
	requireOK(t, err)
	if !cfg.EnvironmentRunner.Enabled || !cfg.EnvironmentRunner.CloudHarness {
		t.Fatal("API configuration must enable the cloud harness and environment runner")
	}
	database, err := db.Open(t.Context(), cfg, nil)
	requireOK(t, err)
	t.Cleanup(database.Close)
	e := &liveEnv{url: strings.TrimRight(baseURL, "/"), apiKey: os.Getenv("TEST_API_KEY"), database: database}
	if e.apiKey == "" {
		e.apiKey = "sk-ant-local-default"
	}
	e.key, err = database.GetAPIKey(t.Context(), auth.HashAPIKey(e.apiKey))
	requireOK(t, err)
	model := provisionHarnessProvider(t, e, cfg, providers)
	marker := "OMA-" + uuid.NewString()[:12]
	queue, tasks := watchHarnessTestQueue(t, cfg.NATS.URL)
	f := createHarnessChat(t, e, model, marker)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	stream, closeStream := connectChatScopedStream(t, ctx, f, "", "agent.message", "agent.thinking")
	defer closeStream()
	primary, found, err := database.GetPrimarySessionThread(ctx, f.session.WorkspaceUUID, f.session.ExternalID)
	requireOK(t, err)
	if !found {
		t.Fatal("session has no primary thread")
	}
	threadStream, closeThreadStream := connectChatScopedStream(t, ctx, f, primary.ExternalID, "agent.message", "agent.thinking")
	defer closeThreadStream()
	plainStream, closePlainStream := connectChatScopedStream(t, ctx, f, "")
	defer closePlainStream()

	initial, err := tasks.NextMsgWithContext(ctx)
	requireOK(t, err)
	initialRef := assertHarnessInputDelivery(t, f, initial.Data, initial.Header, marker)
	if _, err := database.HarnessTurnState(ctx, initialRef); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("initial input created a turn before consumption: %v", err)
	}
	input, delivery := retryHarnessPublish(t, ctx, f, queue)
	ref := assertHarnessInputDelivery(t, f, delivery.Data, delivery.Header, "verification code I gave you")
	if _, err := database.ClaimHarnessTurn(ctx, ref, db.HarnessTurnDelivery{}, "test hash", time.Now().Add(time.Minute)); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("later input could claim before the initial input: %v", err)
	}
	if _, err := database.HarnessTurnState(ctx, ref); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("rejected claim left a turn behind: %v", err)
	}
	t.Log("direct publish failed with 503 and the same input was published using its original idempotency key")

	runHarnessWorker(t, ctx, worker, e.url, "harness turn acknowledged")
	first := receiveHarnessReply(t, ctx, stream, true)
	assertHarnessReply(t, first, "READY")
	threadReply := receiveHarnessReply(t, ctx, threadStream, true)
	plainReply := receiveHarnessReply(t, ctx, plainStream, false)
	if threadReply.ID != first.ID || plainReply.ID != first.ID {
		t.Fatal("session, thread, and non-opt-in subscribers received different final replies")
	}
	t.Log("session and primary-thread previews reconciled with the same reply received by a non-opt-in subscriber")

	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unavailable.Close()
	runHarnessWorker(t, ctx, worker, unavailable.URL, "harness turn needs redelivery")
	runHarnessWorker(t, ctx, worker, e.url, "harness turn acknowledged")
	second := receiveHarnessReply(t, ctx, stream, true)
	assertHarnessReply(t, second, marker)
	t.Log("JetStream redelivered the second turn and a new Worker recovered the verification code")
	_, err = queue.Publish(ctx, delivery.Subject, delivery.Data)
	requireOK(t, err)
	runHarnessWorker(t, ctx, worker, e.url, "harness settled delivery acknowledged")
	assertHarnessHistory(t, f, input, first, second)
	assertHarnessThreadHistory(t, f, primary.ExternalID)
	runHarnessWorker(t, ctx, worker, e.url, "no harness input available", "-wait=1s")
	t.Log("both replies appear once in history and the pull consumer is empty")
}

func receiveHarnessReply(t *testing.T, ctx context.Context, events <-chan chatEvent, wantsPreview bool) chatEvent {
	t.Helper()
	var requestID, previewID string
	var final chatEvent
	var inputConfirmed bool
	fragments := make(map[int]string)
	thinking := make(map[string]bool)
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for Harness SSE")
		case event, ok := <-events:
			if !ok {
				t.Fatal("Harness SSE ended before model_request_end")
			}
			switch event.Type {
			case "user.message":
				inputConfirmed = inputConfirmed || event.Processed != nil
			case "span.model_request_start":
				if !inputConfirmed {
					t.Fatal("model started before the input confirmation reached SSE")
				}
				requestID = event.ID
			case "event_start":
				if !wantsPreview || requestID == "" || event.Event.ID == "" || event.ID != "" || event.Processed != nil {
					t.Fatalf("unexpected preview start: %+v", event)
				}
				if event.Event.Type == "agent.thinking" {
					thinking[event.Event.ID] = false
				} else if event.Event.Type == "agent.message" && previewID == "" && final.ID == "" {
					previewID = event.Event.ID
				} else {
					t.Fatalf("unexpected preview start: %+v", event.Event)
				}
			case "event_delta":
				if !wantsPreview || previewID == "" || event.EventID != previewID || final.ID != "" || event.ID != "" ||
					event.Processed != nil || event.Delta.Index == nil || *event.Delta.Index < 0 ||
					event.Delta.Type != "content_delta" || event.Delta.Content.Type != "text" || event.Delta.Content.Text == "" {
					t.Fatalf("unexpected preview delta: %+v", event)
				}
				fragments[*event.Delta.Index] += event.Delta.Content.Text
			case "agent.thinking":
				if len(event.Content) != 0 {
					t.Fatal("thinking event exposed content")
				}
				if _, previewed := thinking[event.ID]; previewed {
					thinking[event.ID] = true
				}
			case "agent.message":
				if requestID == "" || final.ID != "" || wantsPreview && (event.ID != previewID || len(fragments) == 0) {
					t.Fatalf("reply %q does not match preview %q", event.ID, previewID)
				}
				for index, prefix := range fragments {
					if index >= len(event.Content) || !strings.HasPrefix(event.Content[index].Text, prefix) {
						t.Fatalf("preview at index %d is not a prefix of the final reply", index)
					}
				}
				final = event
			case "span.model_request_end":
				if final.ID == "" || event.ID != requestID+"_end" {
					t.Fatalf("model request ended before its reply: %q", event.ID)
				}
				for id, reconciled := range thinking {
					if !reconciled {
						t.Fatalf("thinking preview %q has no buffered event", id)
					}
				}
				return final
			}
		}
	}
}

func retryHarnessPublish(t *testing.T, ctx context.Context, f *liveSession, queue jetstream.JetStream) (string, *jetstream.RawStreamMsg) {
	t.Helper()
	stream, err := queue.Stream(ctx, "OMA_HARNESS_TASKS")
	requireOK(t, err)
	info, err := stream.Info(ctx)
	requireOK(t, err)
	restore := func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, err := queue.UpdateStream(cleanup, info.Config)
		requireOK(t, err)
	}
	t.Cleanup(restore)
	reject := info.Config
	reject.MaxMsgSize = 128
	_, err = queue.UpdateStream(ctx, reject)
	requireOK(t, err)

	const text = "Reply with only the verification code I gave you in my previous message."
	key := uuid.NewString()
	submitHarnessChat(t, ctx, f, key, text, http.StatusServiceUnavailable)
	var accepted string
	for _, event := range chatHistory(t, f) {
		if len(event.Content) == 1 && event.Content[0].Text == text {
			accepted = event.ID
		}
	}
	if accepted == "" {
		t.Fatal("503 did not preserve the accepted public input")
	}
	restore()
	if id := submitHarnessChat(t, ctx, f, key, text, http.StatusOK); id != accepted {
		t.Fatalf("retry returned input %q, expected %q", id, accepted)
	}
	delivery, err := stream.GetLastMsgForSubject(ctx, "oma.harness.task.v1")
	requireOK(t, err)
	return accepted, delivery
}

func submitHarnessChat(t *testing.T, ctx context.Context, f *liveSession, key, text string, status int) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"events": []any{
		map[string]any{"type": "user.message", "content": []map[string]string{{"type": "text", "text": text}}},
	}})
	requireOK(t, err)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.env.url+"/v1/sessions/"+f.session.ExternalID+"/events?beta=true", bytes.NewReader(body))
	requireOK(t, err)
	request.Header.Set("Authorization", "Bearer "+f.env.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	request.Header.Set("Idempotency-Key", key)
	response, err := http.DefaultClient.Do(request)
	requireOK(t, err)
	defer response.Body.Close()
	if response.StatusCode != status {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		t.Fatalf("input returned %d, want %d: %s", response.StatusCode, status, data)
	}
	if status != http.StatusOK {
		return ""
	}
	var result struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.NewDecoder(response.Body).Decode(&result))
	if len(result.Data) != 1 {
		t.Fatalf("retry returned %d accepted events", len(result.Data))
	}
	return result.Data[0].ID
}

func assertHarnessInputDelivery(t *testing.T, f *liveSession, body []byte, header nats.Header, text string) db.HarnessTurnRef {
	t.Helper()
	var envelope struct {
		Version int               `json:"version"`
		Turn    db.HarnessTurnRef `json:"turn"`
		Kind    string            `json:"kind"`
		Input   struct {
			GroupID string `json:"group_id"`
			Text    string `json:"text"`
		} `json:"input"`
	}
	requireOK(t, json.Unmarshal(body, &envelope))
	ref := envelope.Turn
	if envelope.Version != 4 || envelope.Kind != "user_input" || envelope.Input.GroupID != f.session.UUID || ref.SessionUUID != f.session.UUID ||
		ref.WorkspaceUUID != f.session.WorkspaceUUID || ref.OrganizationUUID != f.session.OrganizationUUID ||
		ref.UUID != header.Get(jetstream.MsgIDHeader) || !strings.Contains(envelope.Input.Text, text) {
		t.Fatalf("unexpected NATS input: %+v", envelope)
	}
	return ref
}

func watchHarnessTestQueue(t *testing.T, url string) (jetstream.JetStream, *nats.Subscription) {
	t.Helper()
	connection, err := nats.Connect(url, nats.Timeout(5*time.Second))
	requireOK(t, err)
	t.Cleanup(connection.Close)
	queue, err := jetstream.New(connection)
	requireOK(t, err)
	tasks, err := connection.SubscribeSync("oma.harness.task.v1")
	requireOK(t, err)
	requireOK(t, connection.FlushTimeout(5*time.Second))
	return queue, tasks
}

func provisionHarnessProvider(t *testing.T, e *liveEnv, cfg config.Config, file string) string {
	t.Helper()
	loginWorkerConsole(t, e, cfg)
	if len(isolatedWorkerProviders(t, e)) != 0 {
		t.Fatal("Harness verification requires an isolated workspace with no providers")
	}
	spec, err := devinit.LoadModelProviders(file)
	requireOK(t, err)
	for _, provider := range spec.Providers {
		if provider.APIBackend != "messages" || len(provider.ModelIDs) == 0 {
			continue
		}
		model := provider.ModelIDs[0]
		var created struct {
			ID string `json:"id"`
		}
		input := map[string]any{
			"name": "Harness real text", "slug": "harness-real", "api_backend": "messages",
			"base_url": provider.BaseURL, "api_key": provider.APIKey, "model_ids": []string{model},
		}
		requireOK(t, json.Unmarshal(e.request(t, http.MethodPost, isolatedProviderPath(e), "", input, http.StatusCreated, e.cookies...), &created))
		t.Cleanup(func() {
			e.requestContext(context.Background(), t, http.MethodDelete, isolatedProviderPath(e)+"/"+created.ID, "", nil, http.StatusNoContent, e.cookies...)
		})
		t.Logf("live model %s", model)
		return llmproviders.QualifiedModelID("harness-real", model)
	}
	t.Fatal("no Messages provider with an explicit model in test provider configuration")
	return ""
}

func createHarnessChat(t *testing.T, e *liveEnv, model, marker string) *liveSession {
	t.Helper()
	create := func(path string, body any) string {
		var result struct {
			ID string `json:"id"`
		}
		requireOK(t, json.Unmarshal(e.request(t, http.MethodPost, path, e.apiKey, body, http.StatusOK), &result))
		if result.ID == "" {
			t.Fatalf("%s did not return an ID", path)
		}
		return result.ID
	}
	agent := create("/v1/agents", map[string]any{"name": "Harness text", "model": map[string]string{"id": model}})
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, http.MethodPost, "/v1/agents/"+agent+"/archive", e.apiKey, nil, http.StatusOK)
	})
	environment := create("/v1/environments", map[string]string{"name": "Harness text"})
	t.Cleanup(func() {
		requireOK(t, e.database.DeleteEnvironment(context.Background(), e.key.WorkspaceUUID.String(), environment))
	})
	input := map[string]any{"type": "user.message", "content": []map[string]string{
		{"type": "text", "text": "Remember this verification code for the next turn: " + marker + ". For this turn reply with only READY."},
	}}
	id := create("/v1/sessions", map[string]any{
		"agent": agent, "environment_id": environment, "title": "Harness text E2E", "initial_events": []any{input},
	})
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, http.MethodPost, "/v1/sessions/"+id+"/archive", e.apiKey, nil, http.StatusOK)
		e.requestContext(context.Background(), t, http.MethodDelete, "/v1/sessions/"+id, e.apiKey, nil, http.StatusOK)
	})
	session, _, err := e.database.GetSession(t.Context(), e.key.WorkspaceUUID.String(), id)
	requireOK(t, err)
	if session.ExecutionRuntime != sessioncontract.CloudHarnessRuntime {
		t.Fatalf("session execution runtime = %q", session.ExecutionRuntime)
	}
	return &liveSession{env: e, session: session}
}

func runHarnessWorker(t *testing.T, ctx context.Context, worker, apiURL, want string, options ...string) {
	t.Helper()
	args := append([]string{"-api-url", apiURL, "-wait=30s", "-timeout=45s"}, options...)
	data, err := exec.CommandContext(ctx, worker, args...).CombinedOutput()
	success := want != "harness turn needs redelivery"
	if (err == nil) != success {
		t.Fatalf("worker success = %t, want %t: %v\n%s", err == nil, success, err, data)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("worker did not report %q:\n%s", want, data)
	}
}

func assertHarnessReply(t *testing.T, event chatEvent, want string) {
	t.Helper()
	if event.ID == "" || len(event.Content) != 1 || strings.TrimSpace(event.Content[0].Text) != want {
		t.Fatalf("want text %q, got %+v", want, event.Content)
	}
}

func assertHarnessHistory(t *testing.T, f *liveSession, input string, first, second chatEvent) {
	t.Helper()
	if first.ID == second.ID {
		t.Fatal("both answers have the same ID")
	}
	var answers []string
	var starts, ends, inputs int
	var idle bool
	for _, event := range chatHistory(t, f) {
		switch event.Type {
		case "event_start", "event_delta":
			t.Fatal("live preview was stored in session history")
		case "agent.message":
			answers = append(answers, event.ID)
		case "user.message":
			if event.Processed == nil || (inputs == 1 && event.ID != input) {
				t.Fatalf("unexpected input history event %s", event.ID)
			}
			inputs++
		case "span.model_request_start":
			starts++
		case "span.model_request_end":
			ends++
		case "session.status_idle":
			idle = event.StopReason.Type == "end_turn"
		}
	}
	if !slices.Equal(answers, []string{first.ID, second.ID}) || inputs != 2 || starts != 2 || ends != 2 || !idle {
		t.Fatalf("unexpected history: answers=%v inputs=%d starts=%d ends=%d idle=%t", answers, inputs, starts, ends, idle)
	}
}

func assertHarnessThreadHistory(t *testing.T, f *liveSession, threadID string) {
	t.Helper()
	var history struct {
		Data []chatEvent `json:"data"`
	}
	path := "/v1/sessions/" + f.session.ExternalID + "/threads/" + threadID + "/events?order=asc&limit=100"
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", path, f.env.apiKey, nil, http.StatusOK), &history))
	answers := 0
	for _, event := range history.Data {
		switch event.Type {
		case "event_start", "event_delta":
			t.Fatal("live preview was stored in thread history")
		case "agent.message":
			answers++
		}
	}
	if answers != 2 {
		t.Fatalf("thread history contains %d replies, want 2", answers)
	}
}
