package liveworker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const chatAnswer = "你好，聊天验证完成。"

type chatEvent struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	ThreadID  string  `json:"session_thread_id"`
	Processed *string `json:"processed_at"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Event struct {
		ID string `json:"id"`
	} `json:"event"`
	EventID string `json:"event_id"`
	Delta   struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"delta"`
}

func TestChatRoundtrip(t *testing.T) {
	if os.Getenv("VERIFY_CHAT_RUN_ID") == "" || os.Getenv("LIVE_WORKER_REAL_CLAUDE") != "1" {
		t.Skip("run through verify-chat with isolated dependencies and a real Worker image")
	}
	started := time.Now()
	proof := func(stage string) {
		data, err := json.Marshal(map[string]any{"stage": stage, "elapsed_ms": time.Since(started).Milliseconds()})
		requireOK(t, err)
		t.Logf("CHAT_PROOF %s", data)
	}
	e := newLiveEnv(t)
	previewSeen := make(chan struct{}, 2)
	var calls atomic.Int32
	model := chatModelFixture(t, previewSeen, &calls)
	configureChatModel(t, e, model.URL)
	f := e.newSessionWithSnapshot(t, json.RawMessage(`{"model":{"id":"claude-sonnet-4-6"}}`))
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 90*time.Second))
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	proof("sse_connected")
	startRealControlWorker(t, f, "")
	var sent struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey,
		map[string]any{"events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": "请回复固定的聊天验证文本。"}}}}}, 200), &sent))
	if len(sent.Data) != 1 || sent.Data[0].ID == "" {
		t.Fatal("input response must contain one stable event ID")
	}
	proof("input_submitted")
	final := receiveChatAnswer(t, ctx, events, func() { previewSeen <- struct{}{} })
	proof("preview_and_final_match")
	closeStream()
	verifyChatHistory(t, f, sent.Data[0].ID, final)
	proof("history_recovered")
	waitRealWorker(t, "session idle and input ACK drained", func() bool {
		var session struct {
			Status string `json:"status"`
		}
		requireOK(t, json.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID, e.apiKey, nil, 200), &session))
		queue := f.consumer(t)
		return session.Status == "idle" && queue.NumAckPending == 0 && queue.NumPending == 0
	})
	if calls.Load() != 1 {
		t.Fatalf("model requests = %d, want one", calls.Load())
	}
	verifyChatHistory(t, f, sent.Data[0].ID, final)
	proof("idle_and_drained")
}

func configureChatModel(t *testing.T, e *liveEnv, modelURL string) {
	t.Helper()
	providers, err := e.database.ListLLMProviders(t.Context(), e.key.OrganizationUUID.String(), e.key.WorkspaceUUID.String())
	requireOK(t, err)
	if len(providers) != 1 || !strings.HasPrefix(providers[0].ExternalID, "llmprov_probe_") {
		t.Fatal("chat verification requires a fresh isolated workspace with its own fixture provider")
	}
	provider := providers[0]
	provider.BaseURL = modelURL
	_, err = e.database.UpdateLLMProvider(t.Context(), provider)
	requireOK(t, err)
}

func connectChatStream(t *testing.T, ctx context.Context, f *liveSession) (<-chan chatEvent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(ctx, "GET", f.env.url+"/v1/sessions/"+f.session.ExternalID+"/events/stream?beta=true&event_deltas[]=agent.message", nil)
	requireOK(t, err)
	req.Header.Set("Authorization", "Bearer "+f.env.apiKey)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	closeStream := sync.OnceFunc(func() { cancel(); _ = response.Body.Close() })
	t.Cleanup(closeStream)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("public SSE status=%d", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if !scanner.Scan() || scanner.Text() != ": connected" {
		t.Fatal("public SSE did not confirm subscription readiness")
	}
	events := make(chan chatEvent, 64)
	go func() {
		defer close(events)
		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var event chatEvent
			if json.Unmarshal([]byte(data), &event) != nil {
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, closeStream
}

func receiveChatAnswer(t *testing.T, ctx context.Context, events <-chan chatEvent, release func()) chatEvent {
	t.Helper()
	var previewID, text string
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for public preview and final response")
		case event, open := <-events:
			if !open {
				t.Fatal("public SSE ended before final response")
			}
			switch event.Type {
			case "event_start":
				previewID = event.Event.ID
			case "event_delta":
				if previewID == "" || event.EventID != previewID {
					t.Fatal("delta missing its matching preview start")
				}
				text += event.Delta.Content.Text
				release()
			case "agent.message":
				if event.ID != previewID || text != chatAnswer || len(event.Content) != 1 || event.Content[0].Text != text || event.ThreadID == "" {
					t.Fatalf("preview/final mismatch: preview=%q final=%+v text=%q", previewID, event, text)
				}
				return event
			}
		}
	}
}

func verifyChatHistory(t *testing.T, f *liveSession, inputID string, final chatEvent) {
	t.Helper()
	for _, scope := range []string{"", "/threads/" + final.ThreadID} {
		verifyChatHistoryScope(t, f, scope, inputID, final)
	}
}

func verifyChatHistoryScope(t *testing.T, f *liveSession, scope, inputID string, final chatEvent) {
	t.Helper()
	var history struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+scope+"/events?order=asc&limit=100", f.env.apiKey, nil, 200), &history))
	var inputs, answers, starts, ends int
	for _, event := range history.Data {
		if (event.ThreadID != "" && event.ThreadID != final.ThreadID) || (scope != "" && event.ThreadID == "") {
			t.Fatalf("history thread mismatch: type=%s thread=%s scope=%s", event.Type, event.ThreadID, scope)
		}
		switch event.Type {
		case "user.message":
			if event.ID != inputID || event.Processed == nil {
				t.Fatalf("history input mismatch: id=%s processed=%v thread=%s scope=%s", event.ID, event.Processed, event.ThreadID, scope)
			}
			inputs++
		case "agent.message":
			if event.ID != final.ID || len(event.Content) != 1 || event.Content[0].Text != chatAnswer {
				t.Fatalf("history differs from live final response: event=%+v scope=%s", event, scope)
			}
			answers++
		case "span.model_request_start":
			starts++
		case "span.model_request_end":
			ends++
		case "event_start", "event_delta":
			t.Fatal("ephemeral preview leaked into persistent history")
		}
	}
	if inputs != 1 || answers != 1 || starts != 1 || ends != 1 {
		t.Fatalf("history counts: input=%d answer=%d model_start=%d model_end=%d", inputs, answers, starts, ends)
	}
}

func chatModelFixture(t *testing.T, previewSeen <-chan struct{}, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(kind string, fields map[string]any) {
			fields["type"] = kind
			data, _ := json.Marshal(fields)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
			w.(http.Flusher).Flush()
		}
		write("message_start", map[string]any{"message": map[string]any{"id": "msg_chat_roundtrip", "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		for _, fragment := range []string{"你好", "，聊天验证完成。"} {
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": fragment}})
			select {
			case <-previewSeen:
			case <-r.Context().Done():
				return
			case <-t.Context().Done():
				return
			}
		}
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server
}
