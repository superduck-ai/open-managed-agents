package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestParseCompletionRejectsEmptyReplies(t *testing.T) {
	for _, testCase := range []struct {
		backend string
		payload string
	}{
		{llmproviders.Messages, `{"content":[{"type":"thinking","text":"x"}]}`},
		{llmproviders.Messages, `not json`},
		{llmproviders.ChatCompletions, `{"choices":[]}`},
	} {
		if _, err := parseCompletion(testCase.backend, []byte(testCase.payload)); !errors.Is(err, errModelFailed) {
			t.Fatalf("%s %s err = %v", testCase.backend, testCase.payload, err)
		}
	}
}

func TestParseCompletionKeepsTextAfterThinking(t *testing.T) {
	messages, err := parseCompletion(llmproviders.Messages, []byte(`{"content":[{"type":"thinking","thinking":"plan"},{"type":"text","text":"{\"scenarios\":[]}"}]}`))
	if err != nil || messages.Text != `{"scenarios":[]}` {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
}

func TestCompleteUsesProtocolOutputLimitWhenUnset(t *testing.T) {
	bodies, unlock := recordedBodies()
	model, upstream := completionPair(t, func(body map[string]any) (int, string) {
		recordBody(bodies, body)
		return http.StatusOK, `{"content":[{"type":"text","text":"ok"}]}`
	})
	completion, err := model.complete(context.Background(), upstream, CompletionRequest{System: "sys", User: "user"})
	recorded := unlock()
	if err != nil || completion.Text != "ok" || len(recorded) != 1 {
		t.Fatalf("text = %q calls = %d err=%v", completion.Text, len(recorded), err)
	}
	if recorded[0]["max_tokens"] != float64(messagesProtocolMaxTokens) || thinkingType(recorded[0]) != "disabled" {
		t.Fatalf("request = %+v", recorded[0])
	}
}

func completionPair(t *testing.T, respond func(map[string]any) (int, string)) (*LLMRubricModel, llmproviders.Upstream) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		status, payload := respond(body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(server.Close)
	return &LLMRubricModel{client: server.Client()}, llmproviders.Upstream{
		BaseURL: server.URL, ModelID: "claude", APIBackend: llmproviders.Messages,
	}
}

type recorded struct {
	mu    sync.Mutex
	items []map[string]any
}

func recordedBodies() (*recorded, func() []map[string]any) {
	store := &recorded{}
	return store, func() []map[string]any {
		store.mu.Lock()
		defer store.mu.Unlock()
		return append([]map[string]any(nil), store.items...)
	}
}

func recordBody(store *recorded, body map[string]any) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.items = append(store.items, body)
}

func thinkingType(body map[string]any) string {
	thinking, _ := body["thinking"].(map[string]any)
	kind, _ := thinking["type"].(string)
	return kind
}

func TestParseCompletionReadsUsage(t *testing.T) {
	messages, err := parseCompletion(llmproviders.Messages, []byte(`{"content":[{"type":"text","text":"{\"score\":80}"}],"usage":{"input_tokens":900,"output_tokens":40}}`))
	if err != nil || messages.Text != `{"score":80}` || messages.InputTokens != 900 || messages.OutputTokens != 40 {
		t.Fatalf("messages = %+v err=%v", messages, err)
	}
	chat, err := parseCompletion(llmproviders.ChatCompletions, []byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":700,"completion_tokens":25}}`))
	if err != nil || chat.Text != "ok" || chat.InputTokens != 700 || chat.OutputTokens != 25 {
		t.Fatalf("chat = %+v err=%v", chat, err)
	}
}
