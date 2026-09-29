package liveworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func isolatedChat(t *testing.T) {
	t.Helper()
	if os.Getenv("VERIFY_CHAT_RUN_ID") == "" || os.Getenv("LIVE_WORKER_REAL_CLAUDE") != "1" {
		t.Skip("requires verify-chat isolated environment")
	}
}

func chatProof(t *testing.T, started time.Time, stage string) {
	t.Helper()
	t.Logf("CHAT_PROOF {\"stage\":%q,\"elapsed_ms\":%d}", stage, time.Since(started).Milliseconds())
}

func sequentialChatModel(t *testing.T, resume <-chan struct{}, calls *atomic.Int32, cadence time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		call := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(kind string, data map[string]any) {
			data["type"] = kind
			body, _ := json.Marshal(data)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, body)
			w.(http.Flusher).Flush()
		}
		write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_sequence_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": "回复"}})
		if cadence > 0 {
			timer := time.NewTimer(cadence)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			case <-t.Context().Done():
				return
			}
		}
		if call == 1 && resume != nil {
			select {
			case <-resume:
			case <-r.Context().Done():
				return
			case <-t.Context().Done():
				return
			}
		}
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": strconv.Itoa(int(call))}})
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server
}

func submitChat(t *testing.T, f *liveSession, text string) string {
	t.Helper()
	var result struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", f.env.apiKey, map[string]any{"events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": text}}}}}, 200), &result))
	if len(result.Data) != 1 || result.Data[0].ID == "" {
		t.Fatal("missing input event ID")
	}
	return result.Data[0].ID
}

func nextChatEvent(t *testing.T, ctx context.Context, events <-chan chatEvent, kind string) chatEvent {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("CHAT_TIMEOUT waiting for %s: %v", kind, ctx.Err())
		case event, ok := <-events:
			if !ok {
				t.Fatalf("stream ended before %s", kind)
			}
			if event.Type == kind {
				return event
			}
		}
	}
}

func chatHistory(t *testing.T, f *liveSession) []chatEvent {
	t.Helper()
	var result struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?order=asc&limit=1000", f.env.apiKey, nil, 200), &result))
	return result.Data
}

func recoveredChatHistory(t *testing.T, f *liveSession) []chatEvent {
	t.Helper()
	var events []chatEvent
	page := ""
	seenPages := map[string]bool{}
	for {
		var result struct {
			Data     []chatEvent `json:"data"`
			NextPage *string     `json:"next_page"`
		}
		path := "/v1/sessions/" + f.session.ExternalID + "/events?order=asc&limit=2&page=" + url.QueryEscape(page)
		requireOK(t, json.Unmarshal(f.env.request(t, "GET", path, f.env.apiKey, nil, 200), &result))
		events = append(events, result.Data...)
		if result.NextPage == nil {
			return events
		}
		page = *result.NextPage
		if page == "" || seenPages[page] {
			t.Fatal("history pagination did not advance")
		}
		seenPages[page] = true
	}
}

func waitChatIdle(t *testing.T, f *liveSession, answers int) {
	t.Helper()
	waitRealWorker(t, "completed chat and drained input queue", func() bool {
		count := 0
		for _, event := range chatHistory(t, f) {
			if event.Type == "agent.message" {
				count++
			}
		}
		var state struct {
			Status string `json:"status"`
		}
		requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID, f.env.apiKey, nil, 200), &state))
		queue := f.consumer(t)
		return count == answers && state.Status == "idle" && queue.NumPending == 0 && queue.NumAckPending == 0
	})
}

func assertChatTurns(t *testing.T, f *liveSession, inputs []string) {
	t.Helper()
	var messages, answers int
	seen := map[string]bool{}
	for _, event := range chatHistory(t, f) {
		if seen[event.ID] {
			t.Fatal("duplicate history ID")
		}
		seen[event.ID] = true
		switch event.Type {
		case "user.message":
			if messages >= len(inputs) || event.ID != inputs[messages] || event.Processed == nil {
				t.Fatal("input ordering or processing mismatch")
			}
			messages++
		case "agent.message":
			answers++
			if len(event.Content) != 1 || event.Content[0].Text != fmt.Sprintf("回复%d", answers) {
				t.Fatalf("unexpected answer %+v", event)
			}
		case "event_start", "event_delta":
			t.Fatal("preview persisted in history")
		}
	}
	if messages != len(inputs) || answers != len(inputs) {
		t.Fatalf("messages=%d answers=%d want=%d", messages, answers, len(inputs))
	}
}

func TestChatReliability(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	resume := make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	t.Cleanup(release)
	var calls atomic.Int32
	model := sequentialChatModel(t, resume, &calls, 0)
	configureChatModel(t, e, model.URL)
	f := e.newSessionWithSnapshot(t, json.RawMessage(`{"model":{"id":"claude-sonnet-4-6"}}`))
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 120*time.Second))
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	worker := startRealControlWorker(t, f, "")
	first := submitChat(t, f, "第一条，请回复。")
	firstPreview := nextChatEvent(t, ctx, events, "event_delta")
	secondText := "第二条，等待第一条完成。"
	response := e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey,
		map[string]any{"events": []any{map[string]any{"type": "user.message", "content": []any{map[string]string{"type": "text", "text": secondText}}}}}, http.StatusConflict)
	var rejection struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	requireOK(t, json.Unmarshal(response, &rejection))
	inputs := 0
	for _, event := range chatHistory(t, f) {
		if event.Type == "user.message" {
			inputs++
			if event.ID != first {
				t.Fatal("rejected busy input was persisted")
			}
		}
	}
	if rejection.Error.Type != "conflict_error" || inputs != 1 || calls.Load() != 1 {
		t.Fatal("busy input rejection changed history or started another model request")
	}
	worker.reconnect(t)
	if calls.Load() != 1 {
		t.Fatal("reconnect duplicated the running model request")
	}
	chatProof(t, started, "busy_input_rejected")
	chatProof(t, started, "worker_stream_reconnected")
	closeStream()
	release()
	waitChatIdle(t, f, 1)
	assertChatTurns(t, f, []string{first})
	chatProof(t, started, "midstream_history_recovered")
	events, closeRecovered := connectChatStream(t, ctx, f)
	defer closeRecovered()
	chatProof(t, started, "public_stream_reconnected")
	second := submitChat(t, f, secondText)
	recoveredHistory := recoveredChatHistory(t, f)
	var recoveredFinal chatEvent
	for _, event := range recoveredHistory {
		if event.ID == firstPreview.EventID {
			recoveredFinal = event
		}
	}
	if recoveredFinal.ID == "" || recoveredFinal.Type != "agent.message" || len(recoveredFinal.Content) != 1 || recoveredFinal.Content[0].Text != "回复1" {
		t.Fatal("disconnected preview was not replaced by the same-ID final from history")
	}
	secondFinal := nextChatEvent(t, ctx, events, "agent.message")
	if secondFinal.ID == recoveredFinal.ID || len(secondFinal.Content) != 1 || secondFinal.Content[0].Text != "回复2" {
		t.Fatal("reconnected stream replayed the old final or lost the new turn")
	}
	waitChatIdle(t, f, 2)
	assertChatTurns(t, f, []string{first, second})
	finalIDs := map[string]int{}
	for _, event := range recoveredChatHistory(t, f) {
		if event.Type == "agent.message" {
			finalIDs[event.ID]++
		}
	}
	if len(finalIDs) != 2 || finalIDs[recoveredFinal.ID] != 1 || finalIDs[secondFinal.ID] != 1 {
		t.Fatal("history and reconnected SSE disagree on final IDs")
	}
	chatProof(t, started, "public_history_reconciled")
	closeRecovered()
	chatProof(t, started, "busy_input_retried")
	requireOK(t, exec.CommandContext(ctx, "docker", "rm", "-f", worker.container).Run())
	oldToken := f.token
	recovered, err := e.service.RecoverManagedAgentCodeSession(ctx, codesessions.ManagedAgentRecoverInput{Session: f.session, CodeSessionID: f.code.ExternalID})
	requireOK(t, err)
	if recovered.WorkerEpoch <= f.code.CurrentWorkerEpoch {
		t.Fatal("recovery did not advance epoch")
	}
	f.code, _, err = e.database.GetCodeSession(ctx, f.code.ExternalID)
	requireOK(t, err)
	f.token = recovered.SessionIngressToken
	f.modelToken = recovered.OAuthAccessToken
	e.request(t, "POST", f.path("/worker/events/delivery"), oldToken, map[string]any{"worker_epoch": "1", "updates": []any{}}, 401)
	e.request(t, "POST", f.path("/worker/register"), f.token, map[string]string{"session_id": f.code.ExternalID}, 200)
	third := submitChat(t, f, "第三条，重启后回复。")
	events, closeAfterRestart := connectChatStream(t, ctx, f)
	defer closeAfterRestart()
	startRealControlWorker(t, f, "")
	nextChatEvent(t, ctx, events, "agent.message")
	waitChatIdle(t, f, 3)
	assertChatTurns(t, f, []string{first, second, third})
	if calls.Load() != 3 {
		t.Fatalf("model calls=%d want 3", calls.Load())
	}
	chatProof(t, started, "worker_restarted")
}

func chatTimeout(t *testing.T, fallback time.Duration) time.Duration {
	t.Helper()
	if value := os.Getenv("VERIFY_CHAT_TIMEOUT"); value != "" {
		duration, err := time.ParseDuration(value)
		requireOK(t, err)
		if duration <= 0 {
			t.Fatal("VERIFY_CHAT_TIMEOUT must be positive")
		}
		return duration
	}
	return fallback
}
