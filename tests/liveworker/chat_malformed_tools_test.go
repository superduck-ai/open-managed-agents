package liveworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const malformedToolInput = "{\"command\":\"printf 'verified 中😀 delivery' > /tmp/oma-control-e2e.txt\",\"timeout\":\x01}"

const malformedToolInputLength = 83

func verifyMalformedTool(t *testing.T, e *liveEnv, client *anthropic.Client, policy string) {
	t.Helper()
	snapshot := fmt.Sprintf(`{"model":{"id":"claude-sonnet-4-6"},"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":%q}}}]}`, policy)
	f := e.newSessionWithSnapshot(t, json.RawMessage(snapshot))
	var calls atomic.Int32
	modelURL := malformedToolFixture(t, &calls)
	configureChatModel(t, e, strings.Replace(modelURL, "host.docker.internal", "127.0.0.1", 1))
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 90*time.Second))
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	worker := startRealControlWorker(t, f, "")
	sendChatSDK(t, client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{
		OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{Type: "user.message", Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{OfText: &anthropic.BetaManagedAgentsTextBlockParam{Type: "text", Text: "Use the requested tool, handle invalid arguments, then reply done."}}}},
	})
	stream := collectMalformedToolTurn(t, ctx, events)
	closeStream()
	waitRealWorker(t, "malformed turn idle and queues drained", func() bool {
		session, err := client.Beta.Sessions.Get(t.Context(), f.session.ExternalID, anthropic.BetaSessionGetParams{})
		requireOK(t, err)
		input, reply := f.consumer(t), realWorkerReplyConsumer(t, f)
		return session.Status == "idle" && input.NumPending == 0 && input.NumAckPending == 0 && reply.NumPending == 0 && reply.NumAckPending == 0
	})
	history := chatHistory(t, f)
	data, err := json.MarshalIndent(struct {
		SessionID string      `json:"session_id"`
		Requests  int32       `json:"model_requests"`
		Stream    []chatEvent `json:"stream_events"`
		History   []chatEvent `json:"events"`
	}{f.session.ExternalID, calls.Load(), stream, history}, "", "  ")
	requireOK(t, err)
	path := filepath.Join(filepath.Dir(os.Getenv("VERIFY_BE_SERVER")), "malformed-tool-"+policy+"-observations.json")
	requireOK(t, os.WriteFile(path, data, 0o600))
	t.Logf("malformed tool observations: %s", path)
	streamCall, streamResult := assertMalformedToolEvents(t, stream)
	historyCall, historyResult := assertMalformedToolEvents(t, history)
	if streamCall.ID != historyCall.ID || streamResult.ID != historyResult.ID || calls.Load() != 2 {
		t.Fatalf("SSE/history IDs or request count differ: stream=%s/%s history=%s/%s calls=%d", streamCall.ID, streamResult.ID, historyCall.ID, historyResult.ID, calls.Load())
	}
	assertToolFile(t, worker, false)
}

func collectMalformedToolTurn(t *testing.T, ctx context.Context, events <-chan chatEvent) []chatEvent {
	t.Helper()
	var collected []chatEvent
	for {
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for malformed tool turn")
		case event, open := <-events:
			if !open {
				t.Fatal("SSE ended before malformed tool turn completed")
			}
			collected = append(collected, event)
			if event.Type == "session.error" {
				t.Fatal("malformed input recovery produced session.error")
			}
			if event.Type == "session.status_idle" {
				return collected
			}
		}
	}
}

func assertMalformedToolEvents(t *testing.T, events []chatEvent) (chatEvent, chatEvent) {
	t.Helper()
	var call, result chatEvent
	var uses, results, answers, idle int
	for _, event := range events {
		switch event.Type {
		case "agent.tool_use":
			uses++
			call = event
			var input workerUnparsedInput
			requireOK(t, json.Unmarshal(call.Input, &input))
			if call.Name != "bash" || call.ID == "" || call.EvaluatedPermission != "deny" || (len(call.Evaluation) != 0 && string(call.Evaluation) != "null") || input.Value.Raw != malformedToolInput || input.Value.Length != malformedToolInputLength {
				t.Fatalf("malformed call invalid: %+v", call)
			}
		case "agent.tool_result":
			results++
			result = event
			if call.ID == "" || result.ToolUseID != call.ID || !result.IsError {
				t.Fatalf("error result lacks preceding call: %+v", result)
			}
		case "agent.message":
			if results != 1 || len(event.Content) != 1 || event.Content[0].Text != "done" {
				t.Fatal("final answer arrived before tool result or had unexpected text")
			}
			answers++
		case "session.status_idle":
			if results != 1 || answers != 1 || event.StopReason.Type != "end_turn" {
				t.Fatalf("turn ended before tool chain completed: %+v", event)
			}
			idle++
		case "session.error", "user.tool_confirmation":
			t.Fatalf("unexpected malformed tool event: %s", event.Type)
		}
	}
	if uses != 1 || results != 1 || answers != 1 || idle != 1 {
		t.Fatalf("malformed event counts: uses=%d results=%d answers=%d idle=%d", uses, results, answers, idle)
	}
	return call, result
}

type workerUnparsedInput struct {
	Value struct {
		Raw    string `json:"raw"`
		Length int    `json:"len"`
	} `json:"__unparsedToolInput"`
}

func malformedToolFixture(t *testing.T, calls *atomic.Int32) string {
	t.Helper()
	return serveRealWorkerFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(http.StatusOK)
			return
		}
		call := calls.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "cannot read fixture request", http.StatusBadRequest)
			return
		}
		if call == 2 {
			assertMalformedModelRequest(t, body)
		} else if call != 1 {
			t.Errorf("unexpected malformed fixture request %d", call)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		writeMalformedModelResponse(w, call)
	}))
}

func assertMalformedModelRequest(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Messages []struct {
			Content []struct {
				Type      string              `json:"type"`
				ID        string              `json:"id"`
				ToolUseID string              `json:"tool_use_id"`
				Input     workerUnparsedInput `json:"input"`
				IsError   bool                `json:"is_error"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Errorf("cannot decode malformed continuation: %v", err)
		return
	}
	var uses, results int
	for _, message := range request.Messages {
		for _, block := range message.Content {
			if block.Type == "tool_use" && block.ID == "toolu_malformed_e2e" {
				t.Logf("Worker rejected input length=%d, UTF-8 bytes=%d", block.Input.Value.Length, len(malformedToolInput))
			}
			if block.Type == "tool_use" && block.ID == "toolu_malformed_e2e" && block.Input.Value.Raw == malformedToolInput && block.Input.Value.Length == malformedToolInputLength {
				uses++
			}
			if block.Type == "tool_result" && block.ToolUseID == "toolu_malformed_e2e" && block.IsError {
				results++
			}
		}
	}
	if uses != 1 || results != 1 {
		t.Errorf("malformed continuation lacks rejected call/result: uses=%d results=%d", uses, results)
	}
}

func writeMalformedModelResponse(w http.ResponseWriter, call int32) {
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(event string, data map[string]any) {
		data["type"] = event
		body, _ := json.Marshal(data)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
	}
	write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_malformed_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}}})
	reason := "end_turn"
	if call == 1 {
		reason = "tool_use"
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_malformed_e2e", "name": "Bash", "input": map[string]any{}}})
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": malformedToolInput}})
	} else {
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "done"}})
	}
	write("content_block_stop", map[string]any{"index": 0})
	write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
	write("message_stop", map[string]any{})
}
