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

func TestHTTPUpstreamRejectsInvalidAnthropicModelMetadata(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		model string
	}{
		{name: "created at is not RFC3339", model: `{"id":"model-1","created_at":"yesterday"}`},
		{name: "max input tokens is negative", model: `{"id":"model-1","max_input_tokens":-1}`},
		{name: "max tokens is negative", model: `{"id":"model-1","max_tokens":-1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[` + test.model + `],"has_more":false}`))
			}))
			defer server.Close()

			upstream := NewHTTPUpstream(config.AnthropicUpstreamConfig{BaseURL: server.URL, APIKey: "test-key"})
			if _, err := upstream.List(context.Background(), ""); !errors.Is(err, errInvalidUpstreamResponse) {
				t.Fatalf("List() error = %v, want invalid upstream response", err)
			}
		})
	}
}

func TestHTTPUpstreamTreatsZeroTokenLimitsAsUnknown(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-1","max_input_tokens":0,"max_tokens":0}]}`))
	}))
	defer server.Close()

	upstream := NewHTTPUpstream(config.AnthropicUpstreamConfig{BaseURL: server.URL, APIKey: "test-key"})
	page, err := upstream.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Models) != 1 {
		t.Fatalf("models = %#v, want one model", page.Models)
	}
	if page.Models[0].MaxInputTokens != nil || page.Models[0].MaxTokens != nil {
		t.Fatalf("token limits = (%v, %v), want unknown", page.Models[0].MaxInputTokens, page.Models[0].MaxTokens)
	}
}

func TestHTTPUpstreamAcceptsEpochCreatedAt(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-1","created_at":0}]}`))
	}))
	defer server.Close()

	upstream := NewHTTPUpstream(config.AnthropicUpstreamConfig{BaseURL: server.URL, APIKey: "test-key"})
	page, err := upstream.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Models) != 1 || page.Models[0].CreatedAt != "1970-01-01T00:00:00Z" {
		t.Fatalf("models = %#v, want epoch created_at", page.Models)
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
						"code_execution":{"supported":true},
						"context_management":{"supported":true,"clear_thinking_20251015":{"supported":true},"clear_tool_uses_20250919":{"supported":true},"compact_20260112":{"supported":true}},
						"effort":{"supported":true,"low":{"supported":true},"medium":{"supported":true},"high":{"supported":true},"xhigh":{"supported":false},"max":{"supported":true}},
						"thinking":{"supported":true,"types":{"enabled":{"supported":true},"adaptive":{"supported":false}}},
						"tool_use":{"supported":false},
						"image_input":{"supported":true},
						"pdf_input":{"supported":false},
						"structured_outputs":{"supported":true},
						"audio_input":{"supported":true,"formats":["wav"]}
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

	inputTokens := 32000
	maxTokens := 4096
	capabilities, err := json.Marshal(page.Models[0].Capabilities)
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	known := page.Models[0].Capabilities.Known()
	for name, capability := range map[string]*bool{
		"code execution":     known.CodeExecution,
		"clear thinking":     known.ClearThinking,
		"clear tool uses":    known.ClearToolUses,
		"compact context":    known.CompactContext,
		"effort":             known.Effort,
		"low effort":         known.LowEffort,
		"medium effort":      known.MediumEffort,
		"high effort":        known.HighEffort,
		"max effort":         known.MaxEffort,
		"thinking":           known.Thinking,
		"thinking enabled":   known.ThinkingEnabled,
		"image input":        known.ImageInput,
		"structured outputs": known.StructuredOutputs,
	} {
		if capability == nil || !*capability {
			t.Fatalf("%s = %v, want true", name, capability)
		}
	}
	for name, capability := range map[string]*bool{
		"adaptive thinking": known.AdaptiveThinking,
		"tool use":          known.ToolUse,
		"pdf input":         known.PDFInput,
		"xhigh effort":      known.XHighEffort,
	} {
		if capability == nil || *capability {
			t.Fatalf("%s = %v, want false", name, capability)
		}
	}
	var capabilityPayload map[string]map[string]any
	if err := json.Unmarshal(capabilities, &capabilityPayload); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if capabilityPayload["image_input"]["supported"] != true {
		t.Fatalf("image_input capability = %#v, want supported=true", capabilityPayload["image_input"])
	}
	if capabilityPayload["audio_input"]["supported"] != true {
		t.Fatalf("audio_input capability = %#v, want supported=true", capabilityPayload["audio_input"])
	}
	if !strings.Contains(string(capabilities), `"formats":["wav"]`) {
		t.Fatalf("capabilities = %s, want audio formats", capabilities)
	}
	page.Models[0].Capabilities = nil
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

func TestHTTPUpstreamAppliesConfiguredModelMapping(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"logical/model","display_name":"Logical Model"}]}`))
	}))
	defer server.Close()

	upstream := NewHTTPUpstream(config.AnthropicUpstreamConfig{
		BaseURL: server.URL,
		APIKey:  "test-key",
		ModelMappings: map[string]string{
			"logical/model": "provider/effective-model",
		},
	})
	page, err := upstream.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(page.Models) != 1 || page.Models[0].ID != "provider/effective-model" || page.Models[0].DisplayName != "provider/effective-model" {
		t.Fatalf("mapped models = %#v", page.Models)
	}
}
