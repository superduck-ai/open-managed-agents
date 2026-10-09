package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"go.uber.org/goleak"
)

type schemaTestTool struct {
	fantasy.AgentTool
	schema json.RawMessage
}

func (t schemaTestTool) InputSchema() json.RawMessage { return t.schema }

func TestNativePromptRepairsNonAdjacentToolHistory(t *testing.T) {
	history := &memoryHistory{messages: []fantasy.Message{
		fantasy.NewUserMessage("old input"),
		{Role: fantasy.MessageRoleAssistant, Content: []fantasy.MessagePart{fantasy.ToolCallPart{ToolCallID: "prior", ToolName: "sandbox_tool", Input: "{}"}}},
		fantasy.NewUserMessage("interleaved input"),
		{Role: fantasy.MessageRoleTool, Content: []fantasy.MessagePart{fantasy.ToolResultPart{ToolCallID: "prior", Output: fantasy.ToolResultOutputContentText{Text: "saved"}}}},
	}}
	model := &scriptedModel{responses: [][]fantasy.StreamPart{textStep("done")}}
	model.before = func(_ int, call fantasy.Call) {
		if len(call.Prompt) != 5 || call.Prompt[2].Role != fantasy.MessageRoleTool || call.Prompt[3].Role != fantasy.MessageRoleUser {
			t.Fatalf("native Crush did not repair adjacency: %#v", call.Prompt)
		}
	}
	engine := newTestEngine(t, model, history, &recordingEvents{})
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "current input"}); err != nil {
		t.Fatal(err)
	}
}

func TestOfficialAnthropicReceivesFullMCPSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"$ref":"#/$defs/path"}},"required":["path"],"$defs":{"path":{"type":"string","minLength":1}},"additionalProperties":false,"allOf":[{"properties":{"path":{"maxLength":64}}}]}`)
	var received struct {
		Tools     []modelTool `json:"tools"`
		MaxTokens int         `json:"max_tokens"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"test\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	provider, err := anthropic.New(anthropic.WithBaseURL(server.URL), anthropic.WithSkipAuth(true))
	if err != nil {
		t.Fatal(err)
	}
	model, err := provider.LanguageModel(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	var effects atomic.Int32
	tool := schemaTestTool{AgentTool: countingTool(&effects, nil), schema: schema}
	engine := newTestEngine(t, model, &memoryHistory{}, &recordingEvents{}, tool)
	options := &anthropic.ProviderOptions{ExtraBody: map[string]any{"max_tokens": 123}}
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "answer", ProviderOptions: fantasy.ProviderOptions{anthropic.Name: options}}); err != nil {
		t.Fatal(err)
	}
	if len(received.Tools) != 1 || received.MaxTokens != 123 {
		t.Fatalf("tools=%d max_tokens=%d", len(received.Tools), received.MaxTokens)
	}
	var want, got any
	if err := json.Unmarshal(schema, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(received.Tools[0].InputSchema, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("schema changed: %s", received.Tools[0].InputSchema)
	}
	if len(options.ExtraBody) != 1 {
		t.Fatal("caller provider options mutated")
	}
}

func TestAttachmentOnlyInputUsesNativeCrush(t *testing.T) {
	model := &scriptedModel{responses: [][]fantasy.StreamPart{textStep("done")}}
	model.before = func(_ int, call fantasy.Call) {
		if len(call.Prompt) != 1 || len(call.Prompt[0].Content) != 2 {
			t.Fatalf("unexpected image prompt: %#v", call.Prompt)
		}
		text, ok := fantasy.AsMessagePart[fantasy.TextPart](call.Prompt[0].Content[0])
		if !ok || text.Text != "" {
			t.Fatal("attachment-only input acquired text")
		}
		file, ok := fantasy.AsMessagePart[fantasy.FilePart](call.Prompt[0].Content[1])
		if !ok || file.MediaType != "image/png" || string(file.Data) != "image bytes" {
			t.Fatalf("attachment lost: %#v", call.Prompt)
		}
	}
	engine := newTestEngine(t, model, &memoryHistory{}, &recordingEvents{})
	if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Files: []fantasy.FilePart{{MediaType: "image/png", Data: []byte("image bytes")}}}); err != nil {
		t.Fatal(err)
	}
}

func TestToolCallbackFailureJoinsNativeExecutor(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	for _, eventType := range []EventType{EventToolCall, EventStepResponse} {
		model := &scriptedModel{responses: [][]fantasy.StreamPart{toolStep("call")}}
		var effects atomic.Int32
		events := &recordingEvents{failType: eventType, failure: errors.New("persistence unavailable")}
		engine := newTestEngine(t, model, &memoryHistory{}, events, countingTool(&effects, nil))
		if _, err := engine.Run(context.Background(), Call{SessionID: "session", RunID: "run", Prompt: "run"}); err == nil {
			t.Fatal("expected callback failure")
		}
		if effects.Load() != 0 {
			t.Fatal("tool ran after callback failure")
		}
	}
}
