package modelcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestHTTPUpstreamRejectsMalformedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":" invalid "}]}`))
	}))
	defer server.Close()

	upstream := NewHTTPUpstream(config.AnthropicUpstreamConfig{BaseURL: server.URL, APIKey: "test-key"})
	if _, err := upstream.List(context.Background(), ""); !errors.Is(err, errInvalidUpstreamResponse) {
		t.Fatalf("List() error = %v, want invalid upstream response", err)
	}
}

func TestHTTPUpstreamMapsOpaqueModelsAndPagination(t *testing.T) {
	t.Parallel()
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[{
				"id":"provider/model.v1",
				"display_name":"Provider Model",
				"description":"General purpose",
				"created_at":"2026-07-24T00:00:00Z",
				"max_input_tokens":32000,
				"max_tokens":4096,
				"capabilities":{
					"thinking":{"supported":true,"types":{"adaptive":{"supported":false}}},
					"tool_use":{"supported":false},
					"image_input":{"supported":true}
				}
			}],
			"has_more":true,
			"last_id":"provider/model.v1"
		}`))
	}))
	defer server.Close()

	upstream := NewHTTPUpstream(config.AnthropicUpstreamConfig{BaseURL: server.URL + "/gateway", APIKey: "test-key"})
	page, err := upstream.List(context.Background(), "provider/previous")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	request := <-requests
	if request.URL.Path != "/gateway/v1/models" {
		t.Fatalf("request path = %q", request.URL.Path)
	}
	if got := request.URL.Query().Get("after_id"); got != "provider/previous" {
		t.Fatalf("after_id = %q", got)
	}
	if got := request.URL.Query().Get("limit"); got != "1000" {
		t.Fatalf("limit = %q", got)
	}
	if request.Header.Get("X-Api-Key") != "test-key" || request.Header.Get("Anthropic-Version") != anthropicAPIVersion {
		t.Fatalf("request headers = %#v", request.Header)
	}

	thinking := true
	adaptiveThinking := false
	toolUse := false
	inputTokens := 32000
	maxTokens := 4096
	capabilities, err := json.Marshal(page.Models[0].Capabilities)
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	if page.Models[0].Capabilities.Thinking == nil || *page.Models[0].Capabilities.Thinking != thinking ||
		page.Models[0].Capabilities.AdaptiveThinking == nil || *page.Models[0].Capabilities.AdaptiveThinking != adaptiveThinking ||
		page.Models[0].Capabilities.ToolUse == nil || *page.Models[0].Capabilities.ToolUse != toolUse {
		t.Fatalf("typed capabilities = %#v", page.Models[0].Capabilities)
	}
	if !strings.Contains(string(capabilities), `"image_input":{"supported":true}`) {
		t.Fatalf("capabilities = %s, want provider capability passthrough", capabilities)
	}
	page.Models[0].Capabilities = Capabilities{}
	want := Page{
		Models: []Model{{
			ID:             "provider/model.v1",
			DisplayName:    "Provider Model",
			Description:    "General purpose",
			CreatedAt:      "2026-07-24T00:00:00Z",
			MaxInputTokens: &inputTokens,
			MaxTokens:      &maxTokens,
		}},
		HasMore: true,
		LastID:  "provider/model.v1",
	}
	if !reflect.DeepEqual(page, want) {
		t.Fatalf("page = %#v, want %#v", page, want)
	}
}
