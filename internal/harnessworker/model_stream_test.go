package harnessworker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

func TestHarnessWorkerProducesModelStream(t *testing.T) {
	facts := codesessions.HarnessModelFacts{EndedAt: time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC),
		Usage: codesessions.ModelRequestUsage{InputTokens: new(int64(3)), OutputTokens: new(int64(5))}}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	frames := make(chan []codesessions.HarnessModelFrame, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get(sessioncontract.HarnessModelStreamHeader)
		if !strings.HasPrefix(key, "1:") {
			t.Error("model request did not select Worker-owned events")
		}
		var request struct {
			MaxTokens int64 `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		} else if request.MaxTokens != 16384 {
			t.Errorf("model output budget = %d, want 16384", request.MaxTokens)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []struct {
			name string
			data string
		}{
			{"message_start", `{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":3,"output_tokens":0}}}`},
			{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":0}`},
			{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`},
			{"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}`},
			{"content_block_stop", `{"type":"content_block_stop","index":1}`},
			{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`},
			{"message_stop", `{"type":"message_stop"}`},
		} {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, event.data)
		}
		_, _ = fmt.Fprintf(w, "\n%s%s %s\n\n", sessioncontract.HarnessModelFactsComment, key, base64.RawURLEncoding.EncodeToString(raw))
	})
	mux.HandleFunc("/v1/code/sessions/test/worker/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(sessioncontract.HarnessTurnHeader) != "turn" {
			t.Error("event stream did not carry the claimed turn")
		}
		decoder := json.NewDecoder(r.Body)
		var received []codesessions.HarnessModelFrame
		for {
			var frame codesessions.HarnessModelFrame
			if err := decoder.Decode(&frame); err != nil {
				if err != io.EOF {
					t.Error(err)
				}
				break
			}
			received = append(received, frame)
		}
		frames <- received
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	worker := Worker{apiURL: server.URL}
	claim := db.HarnessTurnClaim{Lease: db.HarnessTurnLease{Ref: db.HarnessTurnRef{UUID: "turn"}},
		CodeSession: db.CodeSession{ExternalID: "test", Model: "test", Metadata: json.RawMessage(`{"config":{"system_prompt":"Test."}}`)}}
	catalog := llmproviders.NewCatalog([]db.LLMProvider{{Slug: "default", Models: db.LLMModels{{ID: "test"}}}})
	config, err := resolveModelConfig(claim, catalog)
	if err != nil {
		t.Fatal(err)
	}
	call := cloudharness.ToolCall{ID: "toolu_list", Name: "Bash", Arguments: `{"command":"ls /"}`}
	result := cloudharness.ToolResult{CallID: call.ID, Text: "bin\netc\nusr"}
	history := []cloudharness.Message{
		cloudharness.UserMessage{Text: "List the root directory."},
		cloudharness.AssistantMessage{Call: call},
		result,
	}
	input := cloudharness.StepInput{Message: cloudharness.InputMessage{GroupID: "thread", Content: result}}
	if err := worker.runModelStream(t.Context(), claim, "integration-token", config, nil, history, input); err != nil {
		t.Fatal(err)
	}
	received := <-frames
	var order []string
	for _, frame := range received {
		order = append(order, frame.Type)
	}
	if !slices.Equal(order, []string{codesessions.HarnessModelOpen, codesessions.HarnessThinkingStart, codesessions.HarnessThinkingEnd, codesessions.HarnessTextDelta, codesessions.HarnessModelFinish}) {
		t.Fatalf("event order = %v", order)
	}
	final := received[4].Result
	encoded, err := json.Marshal(received)
	if err != nil || final == nil || final.Next != codesessions.HarnessModelDone || !final.Facts.EndedAt.Equal(facts.EndedAt) ||
		!slices.Equal(final.Blocks, []codesessions.HarnessModelBlock{{Index: 0, Thinking: true}, {Index: 1, Text: "answer"}}) ||
		strings.Contains(string(encoded), "private") {
		t.Fatalf("unexpected final stream result: %s, %v", encoded, err)
	}
}
