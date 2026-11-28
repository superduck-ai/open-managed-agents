package harnessworker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestHarnessModelSendsTimeReminderAfterToolResult(t *testing.T) {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type modelRequest struct {
		Model     string      `json:"model"`
		MaxTokens int64       `json:"max_tokens"`
		System    []textBlock `json:"system"`
		Messages  []struct {
			Role    string      `json:"role"`
			Content []textBlock `json:"content"`
		} `json:"messages"`
	}
	requests := make(chan modelRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request modelRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"request captured"}}`))
	}))
	defer server.Close()
	worker := Worker{apiURL: server.URL}
	claim := db.HarnessTurnClaim{CodeSession: db.CodeSession{Model: "selected/test", Metadata: json.RawMessage(`{"config":{"system_prompt":"Work carefully."}}`)}}
	catalog := llmproviders.NewCatalog([]db.LLMProvider{
		{Slug: "other", Models: db.LLMModels{{ID: "test", MaxOutputTokens: new(2048)}}},
		{Slug: "selected", Models: db.LLMModels{{ID: "test", MaxOutputTokens: new(8192)}}},
	})
	config, err := resolveModelConfig(claim, catalog)
	if err != nil {
		t.Fatal(err)
	}
	model, err := worker.model(t.Context(), "turn", "test-token", config, nil, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	history := []cloudharness.Message{
		cloudharness.UserMessage{Text: "Run the test."},
		cloudharness.AssistantMessage{Call: cloudharness.ToolCall{ID: "toolu_test", Name: "Bash", Arguments: `{"command":"go test"}`}},
		cloudharness.ToolResult{CallID: "toolu_test", Text: "passed"},
	}
	if _, err := model.Complete(t.Context(), history); err == nil {
		t.Fatal("expected the mock gateway to reject the captured request")
	}
	var request modelRequest
	select {
	case request = <-requests:
	default:
		t.Fatal("the model request did not reach the gateway")
	}
	if request.Model != "selected/test" || request.MaxTokens != 8192 {
		t.Fatalf("request model = %q, max_tokens = %d, want selected/test and 8192", request.Model, request.MaxTokens)
	}
	if len(request.System) != 1 || !strings.Contains(request.System[0].Text, "# Agent instructions\n\nWork carefully.") ||
		strings.Contains(request.System[0].Text, "<system-reminder>") {
		t.Fatalf("unexpected system prompt: %+v", request.System)
	}
	if len(request.Messages) != 3 || len(request.Messages[0].Content) != 1 || request.Messages[0].Content[0].Text != "Run the test." {
		t.Fatalf("user message changed: %+v", request.Messages)
	}
	next := request.Messages[2]
	if next.Role != "user" || len(next.Content) != 2 || next.Content[0].Type != "tool_result" ||
		next.Content[1].Type != "text" || !strings.HasPrefix(next.Content[1].Text, "<system-reminder>\n") ||
		!strings.Contains(next.Content[1].Text, "+08:00[Asia/Shanghai]") || !strings.Contains(next.Content[1].Text, "Browser time zone for this request: unavailable") {
		t.Fatalf("unexpected tool result or reminder: %+v", next)
	}
}
