package messages

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

type gatewayTestSearcher struct {
	queries []string
	results []websearch.Result
	err     error
}

func (s *gatewayTestSearcher) Search(_ context.Context, query string, _ websearch.SearchOptions) ([]websearch.Result, error) {
	s.queries = append(s.queries, query)
	return s.results, s.err
}

func TestGatewayMalformedRequestIsTransparent(t *testing.T) {
	gateway := newGateway(config.Config{}, nil, &gatewayTestSearcher{})
	_, handled, err := gateway.handle(context.Background(), []byte("{"), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want transparent fallback", handled, err)
	}
}

func TestGatewayWithoutProviderIsTransparent(t *testing.T) {
	gateway := newGateway(config.Config{}, nil, nil)
	_, handled, err := gateway.handle(context.Background(), []byte("{\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want transparent fallback", handled, err)
	}
}

func TestGatewayToolLoopLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"type\":\"message\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_loop\",\"name\":\"web_search\",\"input\":{\"query\":\"query\"}}],\"stop_reason\":\"tool_use\"}")
	}))
	defer upstream.Close()
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxToolLoops: 1}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{})
	response, handled, err := gateway.handle(context.Background(), []byte("{\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	if !handled || err == nil || response.body != nil {
		t.Fatalf("response = %#v, handled = %v, err = %v; want bounded loop error", response, handled, err)
	}
}

func TestGatewayUpstreamFailureIsPassedThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "{\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"try later\"}}")
	}))
	defer upstream.Close()
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{})
	response, handled, err := gateway.handle(context.Background(), []byte("{\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	wantBody := "{\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"try later\"}}"
	if err != nil || !handled || response.statusCode != http.StatusTooManyRequests || string(response.body) != wantBody {
		t.Fatalf("response = %#v, handled = %v, err = %v; want original upstream error", response, handled, err)
	}
}

func TestGatewayToolLoopProjectsTranscript(t *testing.T) {
	var requests []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			_, _ = io.WriteString(w, "{\"id\":\"msg_tool\",\"type\":\"message\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"web_search\",\"input\":{\"query\":\"golang release\"}}],\"stop_reason\":\"tool_use\"}")
			return
		}
		_, _ = io.WriteString(w, "{\"id\":\"msg_final\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}")
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{results: []websearch.Result{{Title: "Go", URL: "https://go.dev", Content: "release"}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxToolLoops: 2}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[{\"role\":\"user\",\"content\":\"search\"}],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := gateway.handle(context.Background(), body, "beta=true", http.Header{"Anthropic-Version": []string{"2023-06-01"}})
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(requests) != 2 || len(searcher.queries) != 1 || searcher.queries[0] != "golang release" {
		t.Fatalf("requests = %d, queries = %#v", len(requests), searcher.queries)
	}
	encodedFirstRequest, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatalf("marshal first request: %v", err)
	}
	if strings.Contains(string(encodedFirstRequest), "tavily-key") {
		t.Fatal("Tavily API key reached the BYOK request")
	}
	tools, ok := requests[0]["tools"].([]any)
	if !ok || len(tools) != 1 || tools[0].(map[string]any)["name"] != searchToolName {
		t.Fatalf("projected tools = %#v", requests[0]["tools"])
	}
	messages, ok := requests[1]["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("continuation messages = %#v", requests[1]["messages"])
	}
	var final map[string]any
	if err := json.Unmarshal(response.body, &final); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	content := final["content"].([]any)
	if content[0].(map[string]any)["type"] != "server_tool_use" || content[1].(map[string]any)["type"] != "web_search_tool_result" {
		t.Fatalf("projected content = %#v", content)
	}
}

func TestGatewayProviderFailureBecomesToolError(t *testing.T) {
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requestCount++
		if requestCount == 1 {
			_, _ = io.WriteString(w, "{\"id\":\"msg_tool\",\"type\":\"message\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"web_search\",\"input\":{\"query\":\"query\"}}],\"stop_reason\":\"tool_use\"}")
			return
		}
		_, _ = io.WriteString(w, "{\"id\":\"msg_final\",\"type\":\"message\",\"content\":[{\"type\":\"text\",\"text\":\"done\"}],\"stop_reason\":\"end_turn\"}")
	}))
	defer upstream.Close()
	searcher := &gatewayTestSearcher{err: errors.New("provider unavailable")}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	gateway := newGateway(cfg, &http.Client{Timeout: time.Second}, searcher)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := gateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	content := decoded["content"].([]any)
	result := content[1].(map[string]any)
	if result["type"] != "web_search_tool_result" || !strings.Contains(string(response.body), "unavailable") {
		t.Fatalf("provider error response = %s", response.body)
	}
}

func TestGatewaySSEResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"id\":\"msg_final\",\"type\":\"message\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"stop_reason\":\"end_turn\",\"usage\":{}}")
	}))
	defer upstream.Close()
	gateway := newGateway(config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}, &http.Client{Timeout: time.Second}, &gatewayTestSearcher{})
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"stream\":true,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := gateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled || !strings.Contains(string(response.body), "event: message_start") || !strings.Contains(string(response.body), "event: message_stop") {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
}
