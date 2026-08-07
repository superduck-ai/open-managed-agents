package messages

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

type webSearchTestSearcher struct {
	queries     []string
	requests    []websearch.SearchRequest
	results     websearch.SearchResponse
	err         error
	validateErr error
	panic       bool
}

func (s *webSearchTestSearcher) Search(_ context.Context, request websearch.SearchRequest) (websearch.SearchResponse, error) {
	s.queries = append(s.queries, request.Query)
	s.requests = append(s.requests, request)
	if s.panic {
		panic("provider failure")
	}
	return s.results, s.err
}

func (s *webSearchTestSearcher) ValidateOptions(websearch.SearchOptions) error {
	return s.validateErr
}

func newSequencedUpstream(t *testing.T, responses ...string) (*httptest.Server, func() int) {
	t.Helper()
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(requestCount.Add(1)) - 1
		if index >= len(responses) {
			t.Errorf("unexpected upstream request %d", index+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responses[index])
	}))
	t.Cleanup(upstream.Close)
	return upstream, func() int { return int(requestCount.Load()) }
}

func TestWebSearchGatewayMalformedRequestIsTransparent(t *testing.T) {
	webSearchGateway := newWebSearchGateway(config.Config{}, nil, &webSearchTestSearcher{}, nil)
	_, handled, err := webSearchGateway.handle(context.Background(), []byte("{"), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want transparent fallback", handled, err)
	}
}

func TestWebSearchGatewayWithoutProviderIsTransparent(t *testing.T) {
	webSearchGateway := newWebSearchGateway(config.Config{}, nil, nil, nil)
	_, handled, err := webSearchGateway.handle(context.Background(), []byte("{\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want transparent fallback", handled, err)
	}
}

func TestWebSearchGatewayRejectsUnsupportedProviderOptionsBeforeUpstream(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t, `{"type":"message","content":[{"type":"text","text":"unexpected"}],"stop_reason":"end_turn"}`)
	searcher := &webSearchTestSearcher{validateErr: errors.New("brave web search does not support domain restrictions")}
	webSearchGateway := newWebSearchGateway(config.Config{
		AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"},
	}, &http.Client{Timeout: time.Second}, searcher, nil)

	_, handled, err := webSearchGateway.handle(context.Background(), []byte(`{
		"messages": [],
		"tools": [{"type":"web_search_20250305","allowed_domains":["example.test"]}]
	}`), "", nil)
	var requestErr *webSearchGatewayRequestError
	if !handled || !errors.As(err, &requestErr) || !strings.Contains(err.Error(), "does not support domain restrictions") {
		t.Fatalf("handled = %v, err = %v; want provider option validation error", handled, err)
	}
	if requestCount() != 0 || len(searcher.requests) != 0 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 0 and 0", requestCount(), len(searcher.requests))
	}
}

func TestWebSearchGatewayLiteralWebSearchTypeIsTransparent(t *testing.T) {
	webSearchGateway := newWebSearchGateway(config.Config{}, nil, &webSearchTestSearcher{}, nil)
	_, handled, err := webSearchGateway.handle(context.Background(), []byte(`{"tools":[{"type":"web_search","name":"web_search"}]}`), "", nil)
	if handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want undocumented literal type to pass through", handled, err)
	}
}

func TestProjectWebSearchFieldsPreservesUnmanagedToolDefinitions(t *testing.T) {
	fields := map[string]json.RawMessage{
		"tools": json.RawMessage(`[{
			"type":"web_search_20260318",
			"name":"web_search",
			"allowed_callers":["direct"],
			"max_uses":2
		},{
			"type":"web_fetch_20260318",
			"name":"web_fetch",
			"custom_option":"preserve"
		},{
			"type":"bash_20250124",
			"name":"bash",
			"input_schema":{"type":"object"}
		},{
			"type":"future_server_tool_20990101",
			"name":"future_server_tool"
		}]`),
	}

	projected, _, err := projectWebSearchFields(fields)
	if err != nil {
		t.Fatalf("project web search fields: %v", err)
	}

	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(projected["tools"], &tools); err != nil {
		t.Fatalf("decode projected tools: %v", err)
	}
	if len(tools) != 4 {
		t.Fatalf("projected tool count = %d, want 4", len(tools))
	}

	var firstName string
	if err := json.Unmarshal(tools[0]["name"], &firstName); err != nil || firstName != upstreamSearchToolName {
		t.Fatalf("projected search tool name = %q, want %q", firstName, upstreamSearchToolName)
	}
	var preservedType, preservedName, customOption string
	if err := json.Unmarshal(tools[1]["type"], &preservedType); err != nil || preservedType != "web_fetch_20260318" {
		t.Fatalf("preserved server tool type = %q, want web_fetch_20260318", preservedType)
	}
	if err := json.Unmarshal(tools[1]["name"], &preservedName); err != nil || preservedName != "web_fetch" {
		t.Fatalf("preserved server tool name = %q, want web_fetch", preservedName)
	}
	if err := json.Unmarshal(tools[1]["custom_option"], &customOption); err != nil || customOption != "preserve" {
		t.Fatalf("preserved server tool option = %q, want preserve", customOption)
	}
	if err := json.Unmarshal(tools[2]["type"], &preservedType); err != nil || preservedType != "bash_20250124" {
		t.Fatalf("preserved client tool type = %q, want bash_20250124", preservedType)
	}
	if err := json.Unmarshal(tools[3]["type"], &preservedType); err != nil || preservedType != "future_server_tool_20990101" {
		t.Fatalf("preserved unknown tool type = %q, want future_server_tool_20990101", preservedType)
	}
}

func TestProjectWebSearchFieldsProjectsForcedSearchToolChoice(t *testing.T) {
	fields := map[string]json.RawMessage{
		"tools": json.RawMessage(`[
			{"type":"web_search_20250305","name":"web_search"},
			{"name":"bash","description":"Run a command","input_schema":{"type":"object"}}
		]`),
		"tool_choice": json.RawMessage(`{"type":"tool","name":"web_search"}`),
	}

	projected, _, err := projectWebSearchFields(fields)
	if err != nil {
		t.Fatalf("project web search fields: %v", err)
	}

	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(projected["tool_choice"], &choice); err != nil {
		t.Fatalf("decode projected tool_choice: %v", err)
	}
	if choice.Type != "tool" || choice.Name != upstreamSearchToolName {
		t.Fatalf("projected tool_choice = %#v, want forced %q", choice, upstreamSearchToolName)
	}
	if got := string(fields["tool_choice"]); got != `{"type":"tool","name":"web_search"}` {
		t.Fatalf("caller tool_choice changed to %s", got)
	}
}

func TestProjectWebSearchFieldsPreservesOtherToolChoice(t *testing.T) {
	fields := map[string]json.RawMessage{
		"tools": json.RawMessage(`[
			{"type":"web_search_20250305","name":"web_search"},
			{"name":"bash","description":"Run a command","input_schema":{"type":"object"}}
		]`),
		"tool_choice": json.RawMessage(`{"type":"tool","name":"bash"}`),
	}

	projected, _, err := projectWebSearchFields(fields)
	if err != nil {
		t.Fatalf("project web search fields: %v", err)
	}
	if got := string(projected["tool_choice"]); got != `{"type":"tool","name":"bash"}` {
		t.Fatalf("projected tool_choice = %s, want caller choice unchanged", got)
	}
}

func TestWebSearchGatewayPauseContinuationRequiresCurrentSearchTool(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer upstream.Close()
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"search"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_cGF1c2U","name":"web_search","input":{"query":"query"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_oma_encoded_cGF1c2U","content":[]}]}]}`)
	_, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	var requestErr *webSearchGatewayRequestError
	if !handled || !errors.As(err, &requestErr) || !strings.Contains(err.Error(), "same web_search tool") {
		t.Fatalf("handled = %v, err = %v; want invalid pause continuation error", handled, err)
	}
	if requestCount.Load() != 0 || len(searcher.requests) != 0 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 0 and 0", requestCount.Load(), len(searcher.requests))
	}
}

func TestWebSearchGatewayServerToolIterationLimitReturnsPauseTurn(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t, `{"id":"msg_paused","type":"message","content":[{"type":"tool_use","id":"toolu_loop","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`)
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	response, handled, err := webSearchGateway.handle(context.Background(), []byte("{\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v; want pause_turn response", response, handled, err)
	}
	if requestCount() != 1 {
		t.Fatalf("upstream requests = %d, want exactly 1", requestCount())
	}
	if len(searcher.requests) != 1 {
		t.Fatalf("search requests = %d, want completed search before pause", len(searcher.requests))
	}
	var decoded struct {
		StopReason string            `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode pause response: %v", err)
	}
	if decoded.StopReason != "pause_turn" || len(decoded.Content) != 2 ||
		!strings.Contains(string(decoded.Content[0]), `"type":"server_tool_use"`) ||
		!strings.Contains(string(decoded.Content[1]), `"type":"web_search_tool_result"`) {
		t.Fatalf("pause response = %s", response.body)
	}
}

func TestWebSearchGatewayExecutesOpaqueUpstreamSearchToolUse(t *testing.T) {
	const upstreamToolUseID = "call_00_wT6ANzoJQ7K6RCEELrbz7636"
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"`+upstreamToolUseID+`","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)

	response, handled, err := webSearchGateway.handle(context.Background(), []byte(`{"messages":[],"tools":[{"type":"web_search_20250305"}]}`), "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v; want completed search response", response, handled, err)
	}
	if requestCount() != 2 || len(searcher.requests) != 1 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 2 and 1", requestCount(), len(searcher.requests))
	}
	externalID, err := serverWebSearchToolUseID(upstreamToolUseID)
	if err != nil {
		t.Fatalf("mint expected server tool use ID: %v", err)
	}
	if !strings.Contains(string(response.body), `"id":"`+externalID+`"`) ||
		!strings.Contains(string(response.body), `"text":"done"`) {
		t.Fatalf("opaque tool-use response = %s", response.body)
	}
}

func TestWebSearchGatewayPauseTurnContinuationReplaysCompletedSearch(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_paused","type":"message","content":[{"type":"tool_use","id":"toolu_pause","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	tools := json.RawMessage(`[{"type":"web_search_20250305","name":"web_search","max_uses":1}]`)
	firstBody, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "search"}},
		"tools":    tools,
	})
	if err != nil {
		t.Fatalf("encode first request: %v", err)
	}
	first, handled, err := webSearchGateway.handle(context.Background(), firstBody, "", nil)
	if err != nil || !handled || first.statusCode != http.StatusOK {
		t.Fatalf("first response = %#v, handled = %v, err = %v", first, handled, err)
	}
	var paused struct {
		StopReason string            `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(first.body, &paused); err != nil || paused.StopReason != "pause_turn" {
		t.Fatalf("decode paused response: body=%s err=%v", first.body, err)
	}
	secondBody, err := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "search"},
			map[string]any{"role": "assistant", "content": paused.Content},
		},
		"tools": tools,
	})
	if err != nil {
		t.Fatalf("encode continuation request: %v", err)
	}
	second, handled, err := webSearchGateway.handle(context.Background(), secondBody, "", nil)
	if err != nil || !handled || second.statusCode != http.StatusOK || !strings.Contains(string(second.body), `"text":"done"`) {
		t.Fatalf("continuation response = %#v, handled = %v, err = %v", second, handled, err)
	}
	if requestCount() != 2 || len(searcher.requests) != 1 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 2 and 1", requestCount(), len(searcher.requests))
	}
}

func TestWebSearchGatewayServerResultOmitsEncryptedContentAndProjectsAvailableMetadata(t *testing.T) {
	execution := webSearchExecution{
		call: webSearchToolCall{id: "toolu_search"},
		results: websearch.SearchResponse{Results: []websearch.Result{{
			Title: "Result", URL: "https://example.com", Snippet: "search snippet", PageAge: "July 28, 2026",
		}}},
	}
	result, err := webSearchServerResultBlock(execution)
	if err != nil {
		t.Fatalf("build server result: %v", err)
	}
	if strings.Contains(string(result), `"encrypted_content"`) ||
		strings.Contains(string(result), `"content":"search snippet"`) {
		t.Fatalf("OMA-managed server result leaked private replay content: %s", result)
	}

	var block webSearchProtocolBlock
	if err := json.Unmarshal(result, &block); err != nil {
		t.Fatalf("decode server result: %v", err)
	}
	projected, err := projectServerResultToClient(block)
	if err != nil {
		t.Fatalf("project server result: %v", err)
	}
	if !strings.Contains(string(projected), `"title":"Result"`) ||
		!strings.Contains(string(projected), `"url":"https://example.com"`) ||
		!strings.Contains(string(projected), `"page_age":"July 28, 2026"`) {
		t.Fatalf("BYOK result lost available search metadata: %s", projected)
	}

	block.Content = json.RawMessage(`[{"type":"web_search_result","title":"Native","url":"https://example.org","encrypted_content":"provider-opaque"}]`)
	projected, err = projectServerResultToClient(block)
	if err != nil {
		t.Fatalf("project provider-owned encrypted content: %v", err)
	}
	if strings.Contains(string(projected), `encrypted_content`) || !strings.Contains(string(projected), `"title":"Native"`) {
		t.Fatalf("BYOK projection handled provider-owned opaque content incorrectly: %s", projected)
	}
}

func TestWebSearchGatewayPauseTurnContinuationExecutesPendingSearch(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"search"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_cGF1c2U","name":"web_search","input":{"query":"query"}}]}],"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":1}]}`)
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 1 || len(searcher.requests) != 1 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 1 and 1", requestCount(), len(searcher.requests))
	}
	if !strings.Contains(string(response.body), `"tool_use_id":"srvtoolu_oma_encoded_cGF1c2U"`) ||
		!strings.Contains(string(response.body), `"text":"done"`) {
		t.Fatalf("pending pause continuation response = %s", response.body)
	}
}

func TestWebSearchGatewayPauseTurnCanRepeat(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_pause_1","type":"message","content":[{"type":"tool_use","id":"toolu_pause_1","name":"oma_web_search","input":{"query":"first"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_pause_2","type":"message","content":[{"type":"tool_use","id":"toolu_pause_2","name":"oma_web_search","input":{"query":"second"}}],"stop_reason":"tool_use"}`,
	)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	tools := json.RawMessage(`[{"type":"web_search_20250305","name":"web_search","max_uses":2}]`)
	firstBody, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "search"}},
		"tools":    tools,
	})
	if err != nil {
		t.Fatalf("encode first request: %v", err)
	}
	first, handled, err := webSearchGateway.handle(context.Background(), firstBody, "", nil)
	if err != nil || !handled || first.statusCode != http.StatusOK {
		t.Fatalf("first response = %#v, handled = %v, err = %v", first, handled, err)
	}
	var firstPause struct {
		StopReason string            `json:"stop_reason"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(first.body, &firstPause); err != nil || firstPause.StopReason != "pause_turn" {
		t.Fatalf("decode first pause: body=%s err=%v", first.body, err)
	}
	secondBody, err := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "search"},
			map[string]any{"role": "assistant", "content": firstPause.Content},
		},
		"tools": tools,
	})
	if err != nil {
		t.Fatalf("encode second request: %v", err)
	}
	second, handled, err := webSearchGateway.handle(context.Background(), secondBody, "", nil)
	if err != nil || !handled || second.statusCode != http.StatusOK {
		t.Fatalf("second response = %#v, handled = %v, err = %v", second, handled, err)
	}
	var secondPause struct {
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(second.body, &secondPause); err != nil || secondPause.StopReason != "pause_turn" {
		t.Fatalf("decode second pause: body=%s err=%v", second.body, err)
	}
	if requestCount() != 2 || len(searcher.requests) != 2 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 2 and 2", requestCount(), len(searcher.requests))
	}
}

func TestWebSearchGatewayServerToolIterationLimitStreamsPauseTurn(t *testing.T) {
	upstream, _ := newSequencedUpstream(t, `{"id":"msg_paused","type":"message","content":[{"type":"tool_use","id":"toolu_stream_pause","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use","usage":{}}`)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 1}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	response, handled, err := webSearchGateway.handle(context.Background(), []byte(`{"stream":true,"messages":[],"tools":[{"type":"web_search_20250305"}]}`), "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	for _, want := range []string{
		`"type":"server_tool_use"`,
		`"type":"web_search_tool_result"`,
		`"stop_reason":"pause_turn"`,
		"event: message_stop",
	} {
		if !strings.Contains(string(response.body), want) {
			t.Fatalf("pause stream missing %q: %s", want, response.body)
		}
	}
}

func TestWebSearchGatewayUpstreamFailureIsPassedThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "{\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"try later\"}}")
	}))
	defer upstream.Close()
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, &webSearchTestSearcher{}, nil)
	response, handled, err := webSearchGateway.handle(context.Background(), []byte("{\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\"}]}"), "", nil)
	wantBody := "{\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"message\":\"try later\"}}"
	if err != nil || !handled || response.statusCode != http.StatusTooManyRequests || string(response.body) != wantBody {
		t.Fatalf("response = %#v, handled = %v, err = %v; want original upstream error", response, handled, err)
	}
}

func TestWebSearchGatewayProviderFailureBecomesToolError(t *testing.T) {
	upstream, _ := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &webSearchTestSearcher{err: errors.New("provider unavailable")}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, logger)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	ctx := httpapi.WithRequestID(context.Background(), "req_gateway_degraded")
	response, handled, err := webSearchGateway.handle(ctx, body, "", http.Header{})
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
	if !strings.Contains(logs.String(), `"request_id":"req_gateway_degraded"`) ||
		!strings.Contains(logs.String(), `"error_code":"unavailable"`) ||
		!strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("degraded search log = %s", logs.String())
	}
}

// panic recovery 属于 HTTP 边界的 recoverMiddleware，gateway 不吞 provider panic。
func TestWebSearchGatewayProviderPanicPropagatesToHTTPBoundary(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use"}`,
	)
	searcher := &webSearchTestSearcher{panic: true}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")

	recovered := func() (value any) {
		defer func() { value = recover() }()
		_, _, _ = webSearchGateway.handle(context.Background(), body, "", http.Header{})
		return nil
	}()

	if recovered == nil {
		t.Fatal("provider panic was swallowed by the gateway; recovery must stay at the HTTP boundary")
	}
	if requestCount() != 1 {
		t.Fatalf("upstream requests = %d, want 1", requestCount())
	}
}

func TestWebSearchGatewayAccumulatesUsageAcrossIterations(t *testing.T) {
	upstream, _ := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use","usage":{"input_tokens":1000,"output_tokens":50,"service_tier":"standard"}}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":1200,"output_tokens":80,"service_tier":"standard"}}`,
	)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "title", URL: "https://example.test"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	var decoded struct {
		Usage struct {
			InputTokens  int64  `json:"input_tokens"`
			OutputTokens int64  `json:"output_tokens"`
			ServiceTier  string `json:"service_tier"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Usage.InputTokens != 2200 || decoded.Usage.OutputTokens != 130 {
		t.Fatalf("usage = %#v, want input_tokens 2200 and output_tokens 130", decoded.Usage)
	}
	if decoded.Usage.ServiceTier != "standard" {
		t.Fatalf("usage.service_tier = %q, want standard", decoded.Usage.ServiceTier)
	}
}

func TestWebSearchGatewaySingleIterationPreservesUpstreamUsage(t *testing.T) {
	upstream, _ := newSequencedUpstream(t,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":7}}`,
	)
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, &webSearchTestSearcher{}, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	if !strings.Contains(string(response.body), `"usage":{"input_tokens":11,"output_tokens":7}`) {
		t.Fatalf("single-iteration usage = %s", response.body)
	}
}

func TestWebSearchGatewayStripsClientAcceptEncoding(t *testing.T) {
	var upstreamAcceptEncoding string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAcceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(w)
		defer compressed.Close()
		_, _ = io.WriteString(compressed, `{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
	}))
	t.Cleanup(upstream.Close)

	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Transport: newProxyTransport(), Timeout: time.Second}, &webSearchTestSearcher{}, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	headers := http.Header{}
	headers.Set("Accept-Encoding", "gzip, deflate, br")

	response, handled, err := webSearchGateway.handle(context.Background(), body, "", headers)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if upstreamAcceptEncoding != "gzip" {
		t.Fatalf("upstream Accept-Encoding = %q, want the Transport-negotiated %q", upstreamAcceptEncoding, "gzip")
	}
	if !strings.Contains(string(response.body), `"text":"done"`) {
		t.Fatalf("gateway response = %s", response.body)
	}
}

func TestExtractWebSearchToolCallsRejectsDuplicateIDs(t *testing.T) {
	body := []byte(`{"content":[{"type":"tool_use","id":"toolu_duplicate","name":"oma_web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_duplicate","name":"bash","input":{"command":"pwd"}}]}`)

	_, err := extractWebSearchToolCalls(body)
	if err == nil || !strings.Contains(err.Error(), `duplicate tool use id "toolu_duplicate"`) {
		t.Fatalf("extract tool calls error = %v, want duplicate id error", err)
	}
}

// 调用方可以合法地声明自己的 web_search 工具；gateway 的投影名必须与它区分开，否则 BYOK
// 会收到两个同名 tool，且无法再按名字判断 tool_use 的归属。
func TestProjectWebSearchFieldsAvoidsCallerToolNameCollision(t *testing.T) {
	fields := map[string]json.RawMessage{
		"tools": json.RawMessage(`[
			{"type":"web_search_20250305","name":"web_search"},
			{"name":"web_search","description":"caller-owned search","input_schema":{"type":"object"}}
		]`),
	}

	projected, _, err := projectWebSearchFields(fields)
	if err != nil {
		t.Fatalf("project web search fields: %v", err)
	}

	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(projected["tools"], &tools); err != nil {
		t.Fatalf("decode projected tools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("projected tool count = %d, want 2", len(tools))
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		var name string
		if err := json.Unmarshal(tool["name"], &name); err != nil {
			t.Fatalf("decode projected tool name: %v", err)
		}
		names = append(names, name)
	}
	if names[0] != upstreamSearchToolName || names[1] != searchToolName {
		t.Fatalf("projected tool names = %v, want [%q %q]", names, upstreamSearchToolName, searchToolName)
	}
}

// 调用方自有的 web_search 工具不属于 gateway：它必须作为普通 client tool 交还调用方，
// 而不是被 gateway 当成搜索请求执行。
func TestWebSearchGatewayLeavesCallerOwnedSearchToolToClient(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_caller_tool","type":"message","content":[{"type":"tool_use","id":"toolu_caller","name":"web_search","input":{"query":"caller query"}}],"stop_reason":"tool_use"}`,
	)
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"model":"model","max_tokens":32,"messages":[],"tools":[` +
		`{"type":"web_search_20250305","name":"web_search"},` +
		`{"name":"web_search","description":"caller-owned search","input_schema":{"type":"object"}}]}`)

	response, handled, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 1 {
		t.Fatalf("upstream requests = %d, want 1", requestCount())
	}
	if len(searcher.requests) != 0 {
		t.Fatalf("provider searches = %d, want the caller-owned tool left untouched", len(searcher.requests))
	}
	if !strings.Contains(string(response.body), `"type":"tool_use"`) ||
		!strings.Contains(string(response.body), `"id":"toolu_caller"`) ||
		strings.Contains(string(response.body), "server_tool_use") {
		t.Fatalf("caller-owned tool response = %s", response.body)
	}
}

func TestWebSearchToolUseIDMappingIsReversible(t *testing.T) {
	serverID, err := serverWebSearchToolUseID("toolu_abc")
	if err != nil {
		t.Fatalf("mint server tool use ID: %v", err)
	}
	if serverID != "srvtoolu_oma_encoded_dG9vbHVfYWJj" {
		t.Fatalf("server tool use ID = %q, want encoded tool ID", serverID)
	}
	upstreamID, err := upstreamWebSearchToolUseID(serverID)
	if err != nil {
		t.Fatalf("resolve upstream tool use ID: %v", err)
	}
	if upstreamID != "toolu_abc" {
		t.Fatalf("upstream tool use ID = %q, want toolu_abc", upstreamID)
	}
}

func TestWebSearchToolUseIDMappingSupportsOpaqueUpstreamIDs(t *testing.T) {
	upstreamIDs := []string{
		"call_00_wT6ANzoJQ7K6RCEELrbz7636",
		"toolu_oma_encoded_abc",
		"srvtoolu_provider_owned",
	}
	seen := make(map[string]string, len(upstreamIDs))
	for _, upstreamID := range upstreamIDs {
		serverID, err := serverWebSearchToolUseID(upstreamID)
		if err != nil {
			t.Fatalf("mint server tool use ID for %q: %v", upstreamID, err)
		}
		if previous, collision := seen[serverID]; collision {
			t.Fatalf("upstream IDs %q and %q both map to %q", previous, upstreamID, serverID)
		}
		seen[serverID] = upstreamID
		resolved, err := upstreamWebSearchToolUseID(serverID)
		if err != nil || resolved != upstreamID {
			t.Fatalf("round trip of %q = %q, err = %v", upstreamID, resolved, err)
		}
	}
}

func TestWebSearchToolUseIDMappingRejectsInvalidShapes(t *testing.T) {
	serverCases := []string{"srvtoolu_abc", "srvtoolu_", "abc", ""}
	for _, externalID := range serverCases {
		if _, err := upstreamWebSearchToolUseID(externalID); err == nil {
			t.Fatalf("upstreamWebSearchToolUseID(%q) error = nil, want rejection", externalID)
		}
	}
	upstreamCases := []string{"", "   "}
	for _, upstreamID := range upstreamCases {
		if _, err := serverWebSearchToolUseID(upstreamID); err == nil {
			t.Fatalf("serverWebSearchToolUseID(%q) error = nil, want rejection", upstreamID)
		}
	}
}

// opaque 编码是双射：任意两个不同的上游 ID 不能映射到同一个 server ID，否则投影后的同一条
// assistant message 会出现重复 tool_use ID。
func TestWebSearchToolUseIDMappingHasNoCollisions(t *testing.T) {
	upstreamIDs := []string{"toolu_abc", "toolu_oma_abc", "toolu_srvtoolu_abc", "toolu_1"}
	seen := make(map[string]string, len(upstreamIDs))
	for _, upstreamID := range upstreamIDs {
		serverID, err := serverWebSearchToolUseID(upstreamID)
		if err != nil {
			t.Fatalf("mint server tool use ID for %q: %v", upstreamID, err)
		}
		if previous, collision := seen[serverID]; collision {
			t.Fatalf("upstream IDs %q and %q both map to %q", previous, upstreamID, serverID)
		}
		seen[serverID] = upstreamID
		resolved, err := upstreamWebSearchToolUseID(serverID)
		if err != nil || resolved != upstreamID {
			t.Fatalf("round trip of %q = %q, err = %v", upstreamID, resolved, err)
		}
	}
}

// Anthropic 按 tool_use_id 匹配 result，因此排序只影响可读性；但每个 tool_use 都必须
// 有配对的 result，丢弃任何一条 result 都会让转录失效。
func TestOrderWebSearchToolResultsKeepsEveryResult(t *testing.T) {
	assistant := json.RawMessage(`{"role":"assistant","content":[` +
		`{"type":"tool_use","id":"toolu_search","name":"oma_web_search","input":{"query":"query"}},` +
		`{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]}`)

	t.Run("failure duplicate ids are all preserved", func(t *testing.T) {
		results := []json.RawMessage{
			json.RawMessage(`{"type":"tool_result","tool_use_id":"toolu_search","content":"first"}`),
			json.RawMessage(`{"type":"tool_result","tool_use_id":"toolu_search","content":"second"}`),
			json.RawMessage(`{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"}`),
		}
		ordered := orderWebSearchToolResults(assistant, results)
		if len(ordered) != len(results) {
			t.Fatalf("ordered results = %d, want %d; a dropped result orphans its tool_use", len(ordered), len(results))
		}
		joined := fmt.Sprintf("%s", ordered)
		for _, want := range []string{"first", "second", "/workspace"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("ordered results = %s, want to contain %q", joined, want)
			}
		}
	})

	t.Run("failure unmatched result is kept", func(t *testing.T) {
		results := []json.RawMessage{
			json.RawMessage(`{"type":"tool_result","tool_use_id":"toolu_unknown","content":"orphan"}`),
			json.RawMessage(`{"type":"tool_result","tool_use_id":"toolu_search","content":"search"}`),
		}
		ordered := orderWebSearchToolResults(assistant, results)
		if len(ordered) != 2 || !strings.Contains(string(ordered[1]), "orphan") {
			t.Fatalf("ordered results = %s, want the unmatched result kept last", ordered)
		}
	})

	t.Run("success results follow tool call order", func(t *testing.T) {
		results := []json.RawMessage{
			json.RawMessage(`{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"}`),
			json.RawMessage(`{"type":"tool_result","tool_use_id":"toolu_search","content":"search"}`),
		}
		ordered := orderWebSearchToolResults(assistant, results)
		if len(ordered) != 2 ||
			!strings.Contains(string(ordered[0]), `"tool_use_id":"toolu_search"`) ||
			!strings.Contains(string(ordered[1]), `"tool_use_id":"toolu_bash"`) {
			t.Fatalf("ordered results = %s, want search before bash", ordered)
		}
	})
}

func TestWebSearchGatewayMixedContinuationRequiresCurrentSearchTool(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount.Add(1)
	}))
	defer upstream.Close()
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"search and inspect"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_c2VhcmNo","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"}]}],"tools":[{"name":"bash","input_schema":{"type":"object"}}]}`)
	_, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	var requestErr *webSearchGatewayRequestError
	if !handled || !errors.As(err, &requestErr) || !strings.Contains(err.Error(), "same web_search tool") {
		t.Fatalf("handled = %v, err = %v; want invalid continuation error", handled, err)
	}
	if requestCount.Load() != 0 || len(searcher.requests) != 0 {
		t.Fatalf("BYOK requests = %d, searches = %d; want 0 and 0", requestCount.Load(), len(searcher.requests))
	}
}

func TestWebSearchGatewayMixedContinuationRejectsDuplicateClientResults(t *testing.T) {
	messages := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"search and inspect"}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_c2VhcmNo","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]}`),
		json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"},{"type":"tool_result","tool_use_id":"toolu_bash","content":"duplicate"}]}`),
	}

	_, err := findPendingWebSearchTurn(messages)
	if err == nil || !strings.Contains(err.Error(), `duplicate client tool result "toolu_bash"`) {
		t.Fatalf("find pending turn error = %v, want duplicate client result error", err)
	}
}

func TestWebSearchGatewayRejectsDuplicateServerToolUsesInReplayedHistory(t *testing.T) {
	t.Run("mixed continuation", func(t *testing.T) {
		messages := []json.RawMessage{
			json.RawMessage(`{"role":"user","content":"search and inspect"}`),
			json.RawMessage(`{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_c2VhcmNo","name":"web_search","input":{"query":"first"}},{"type":"server_tool_use","id":"srvtoolu_oma_encoded_c2VhcmNo","name":"web_search","input":{"query":"second"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]}`),
			json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"}]}`),
		}

		_, err := findPendingWebSearchTurn(messages)
		if err == nil || !strings.Contains(err.Error(), `duplicate server web search tool use id "srvtoolu_oma_encoded_c2VhcmNo"`) {
			t.Fatalf("find pending turn error = %v, want duplicate server tool use error", err)
		}
	})

	t.Run("paused continuation", func(t *testing.T) {
		messages := []json.RawMessage{
			json.RawMessage(`{"role":"user","content":"search"}`),
			json.RawMessage(`{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_c2VhcmNo","name":"web_search","input":{"query":"first"}},{"type":"server_tool_use","id":"srvtoolu_oma_encoded_c2VhcmNo","name":"web_search","input":{"query":"second"}}]}`),
		}

		_, err := findPausedWebSearchTurn(messages)
		if err == nil || !strings.Contains(err.Error(), `duplicate server web search tool use id "srvtoolu_oma_encoded_c2VhcmNo"`) {
			t.Fatalf("find paused turn error = %v, want duplicate server tool use error", err)
		}
	})
}

func TestWebSearchGatewayMixedContinuationRejectsNonToolResultContent(t *testing.T) {
	messages := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"search and inspect"}`),
		json.RawMessage(`{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_c2VhcmNo","name":"web_search","input":{"query":"query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}]}`),
		json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"},{"type":"text","text":"continue"}]}`),
	}

	_, err := findPendingWebSearchTurn(messages)
	if err == nil || !strings.Contains(err.Error(), "only tool_result blocks") {
		t.Fatalf("find pending turn error = %v, want non-tool-result content error", err)
	}
}

func TestWebSearchGatewayClientToolUsePassesToClaudeCode(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}}],"stop_reason":"tool_use"}`,
	)
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}}]}`)
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 1 || len(searcher.requests) != 0 {
		t.Fatalf("upstream requests = %d, searches = %d; want 1 and 0", requestCount(), len(searcher.requests))
	}
	if !strings.Contains(string(response.body), `"id":"toolu_bash"`) || strings.Contains(string(response.body), "unsupported tool use") {
		t.Fatalf("client tool response = %s", response.body)
	}
}
func TestWebSearchGatewayMixedToolUseDefersSearchUntilClientResults(t *testing.T) {
	var requestCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if requestNumber == 1 {
			_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"checking"},{"type":"tool_use","id":"toolu_search_1","name":"oma_web_search","input":{"query":"first query"}},{"type":"tool_use","id":"toolu_bash","name":"bash","input":{"command":"pwd"}},{"type":"tool_use","id":"toolu_search_2","name":"oma_web_search","input":{"query":"second query"}},{"type":"tool_use","id":"toolu_read","name":"read_file","input":{"path":"README.md"}}],"stop_reason":"tool_use"}`)
			return
		}
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode continuation: %v", err)
			return
		}
		if len(request.Messages) != 3 {
			t.Errorf("continuation messages = %d, want 3", len(request.Messages))
			return
		}
		assistant := request.Messages[1]
		var assistantContent []json.RawMessage
		if err := json.Unmarshal(assistant.Content, &assistantContent); err != nil {
			t.Errorf("decode assistant content: %v", err)
			return
		}
		if len(assistantContent) != 5 ||
			!strings.Contains(string(assistantContent[1]), `"id":"toolu_search_1"`) ||
			!strings.Contains(string(assistantContent[2]), `"id":"toolu_bash"`) ||
			!strings.Contains(string(assistantContent[3]), `"id":"toolu_search_2"`) ||
			!strings.Contains(string(assistantContent[4]), `"id":"toolu_read"`) ||
			strings.Contains(string(assistant.Content), "server_tool_use") {
			t.Errorf("projected assistant message = %s", assistant.Content)
			return
		}
		last := request.Messages[2]
		var lastContent []json.RawMessage
		if err := json.Unmarshal(last.Content, &lastContent); err != nil {
			t.Errorf("decode continuation results: %v", err)
			return
		}
		if len(lastContent) != 4 {
			t.Errorf("continuation results = %d, want 4", len(lastContent))
			return
		}
		if !strings.Contains(string(lastContent[0]), `"tool_use_id":"toolu_search_1"`) ||
			!strings.Contains(string(lastContent[1]), `"tool_use_id":"toolu_bash"`) ||
			!strings.Contains(string(lastContent[2]), `"tool_use_id":"toolu_search_2"`) ||
			!strings.Contains(string(lastContent[3]), `"tool_use_id":"toolu_read"`) {
			t.Errorf("continuation results = %s", lastContent)
			return
		}
		if strings.Contains(string(lastContent[1]), `"is_error":true`) || strings.Contains(string(lastContent[3]), `"is_error":true`) {
			t.Errorf("client tool results were replaced: %s", lastContent)
			return
		}
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.com", Snippet: "snippet"}}}}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	tools := json.RawMessage(`[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}},{"name":"read_file","input_schema":{"type":"object"}}]`)
	body := []byte(`{"messages":[{"role":"user","content":"search and inspect"}],"tools":[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}},{"name":"read_file","input_schema":{"type":"object"}}]}`)
	first, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || first.statusCode != http.StatusOK || requestCount.Load() != 1 {
		t.Fatalf("first response = %#v, handled = %v, err = %v, requests = %d", first, handled, err, requestCount.Load())
	}
	if len(searcher.requests) != 0 {
		t.Fatalf("searches before client result = %d, want 0", len(searcher.requests))
	}
	var firstMessage struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
	}
	if err := json.Unmarshal(first.body, &firstMessage); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstMessage.StopReason != "tool_use" || len(firstMessage.Content) != 5 {
		t.Fatalf("first response = %s", first.body)
	}
	serverCalls := make([]webSearchContentBlock, 0, 2)
	for _, index := range []int{1, 3} {
		var serverCall webSearchContentBlock
		if err := json.Unmarshal(firstMessage.Content[index], &serverCall); err != nil {
			t.Fatalf("decode server tool use %d: %v", index, err)
		}
		if serverCall.Type != "server_tool_use" || !strings.HasPrefix(serverCall.ID, "srvtoolu_") {
			t.Fatalf("server tool use %d = %s", index, firstMessage.Content[index])
		}
		serverCalls = append(serverCalls, serverCall)
	}
	if !strings.Contains(string(firstMessage.Content[2]), `"id":"toolu_bash"`) ||
		!strings.Contains(string(firstMessage.Content[4]), `"id":"toolu_read"`) ||
		strings.Contains(string(first.body), "web_search_tool_result") {
		t.Fatalf("mixed response = %s", first.body)
	}
	assistant, err := json.Marshal(struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: "assistant", Content: firstMessage.Content})
	if err != nil {
		t.Fatalf("encode assistant continuation: %v", err)
	}
	clientResult := json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_read","content":"readme"},{"type":"tool_result","tool_use_id":"toolu_bash","content":"/workspace"}]}`)
	followUp, err := json.Marshal(map[string]any{
		"messages": []json.RawMessage{json.RawMessage(`{"role":"user","content":"search and inspect"}`), assistant, clientResult},
		"tools":    tools,
	})
	if err != nil {
		t.Fatalf("encode follow-up request: %v", err)
	}
	second, handled, err := webSearchGateway.handle(context.Background(), followUp, "", nil)
	if err != nil || !handled || second.statusCode != http.StatusOK || requestCount.Load() != 2 {
		t.Fatalf("second response = %#v, handled = %v, err = %v, requests = %d", second, handled, err, requestCount.Load())
	}
	if len(searcher.requests) != 2 || searcher.requests[0].Query != "first query" || searcher.requests[1].Query != "second query" {
		t.Fatalf("deferred search requests = %#v", searcher.requests)
	}
	var secondMessage struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(second.body, &secondMessage); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	// 官方规定 mixed continuation 的下一条响应不重复 server_tool_use block；这里只检查
	// content，因为 usage.server_tool_use 是合法的同名计量字段。
	if len(secondMessage.Content) != 3 || !strings.Contains(string(secondMessage.Content[0]), `"type":"web_search_tool_result"`) ||
		!strings.Contains(string(secondMessage.Content[0]), `"tool_use_id":"`+serverCalls[0].ID+`"`) ||
		!strings.Contains(string(secondMessage.Content[1]), `"tool_use_id":"`+serverCalls[1].ID+`"`) ||
		strings.Contains(fmt.Sprintf("%s", secondMessage.Content), "server_tool_use") {
		t.Fatalf("resumed mixed response = %s", second.body)
	}
	if !strings.Contains(string(second.body), `"server_tool_use":{"web_search_requests":2}`) {
		t.Fatalf("resumed mixed usage = %s, want two billable web search requests", second.body)
	}
}

func TestWebSearchGatewayProjectsCompletedSearchHistoryBackToBYOK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(request.Messages) != 5 {
			t.Errorf("projected history messages = %d, want 5", len(request.Messages))
			return
		}
		if request.Messages[1].Role != "assistant" || !strings.Contains(string(request.Messages[1].Content), `"type":"tool_use"`) ||
			!strings.Contains(string(request.Messages[1].Content), `"id":"history"`) {
			t.Errorf("projected search call = %s", request.Messages[1].Content)
			return
		}
		if request.Messages[2].Role != "user" || !strings.Contains(string(request.Messages[2].Content), `"type":"tool_result"`) ||
			!strings.Contains(string(request.Messages[2].Content), `"tool_use_id":"history"`) {
			t.Errorf("projected search result = %s", request.Messages[2].Content)
			return
		}
		if request.Messages[3].Role != "assistant" || !strings.Contains(string(request.Messages[3].Content), `"text":"old answer"`) {
			t.Errorf("projected final content = %s", request.Messages[3].Content)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"new answer"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"old search"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_aGlzdG9yeQ","name":"web_search","input":{"query":"old query"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_oma_encoded_aGlzdG9yeQ","content":[{"type":"web_search_result","title":"Old","url":"https://example.com"}]},{"type":"text","text":"old answer"}]},{"role":"user","content":"new question"}],"tools":[]}`)
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(searcher.requests) != 0 {
		t.Fatalf("history replay searches = %d, want 0", len(searcher.requests))
	}
}

func TestWebSearchGatewayProjectsCompletedMixedHistoryBackToBYOK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(request.Messages) != 5 {
			t.Errorf("projected mixed history messages = %d, want 5", len(request.Messages))
			return
		}
		var calls []json.RawMessage
		if err := json.Unmarshal(request.Messages[1].Content, &calls); err != nil || len(calls) != 2 {
			t.Errorf("projected mixed calls = %s, err = %v", request.Messages[1].Content, err)
			return
		}
		if !strings.Contains(string(calls[0]), `"id":"history_search"`) ||
			!strings.Contains(string(calls[1]), `"id":"toolu_history_bash"`) {
			t.Errorf("projected mixed calls = %s", calls)
			return
		}
		var results []json.RawMessage
		if err := json.Unmarshal(request.Messages[2].Content, &results); err != nil || len(results) != 2 {
			t.Errorf("projected mixed results = %s, err = %v", request.Messages[2].Content, err)
			return
		}
		if !strings.Contains(string(results[0]), `"tool_use_id":"history_search"`) ||
			!strings.Contains(string(results[1]), `"tool_use_id":"toolu_history_bash"`) {
			t.Errorf("projected mixed result order = %s", results)
			return
		}
		if request.Messages[3].Role != "assistant" || !strings.Contains(string(request.Messages[3].Content), `"text":"old answer"`) {
			t.Errorf("projected mixed answer = %s", request.Messages[3].Content)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"new answer"}],"stop_reason":"end_turn"}`)
	}))
	defer upstream.Close()
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[{"role":"user","content":"old mixed turn"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_oma_encoded_aGlzdG9yeV9zZWFyY2g","name":"web_search","input":{"query":"old query"}},{"type":"tool_use","id":"toolu_history_bash","name":"bash","input":{"command":"pwd"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_history_bash","content":"/workspace"}]},{"role":"assistant","content":[{"type":"web_search_tool_result","tool_use_id":"srvtoolu_oma_encoded_aGlzdG9yeV9zZWFyY2g","content":[{"type":"web_search_result","title":"Old","url":"https://example.com"}]},{"type":"text","text":"old answer"}]},{"role":"user","content":"new question"}],"tools":[{"type":"web_search_20250305"},{"name":"bash","input_schema":{"type":"object"}}]}`)
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(searcher.requests) != 0 {
		t.Fatalf("completed mixed history searches = %d, want 0", len(searcher.requests))
	}
}

func TestProjectCompletedWebSearchContentUsesResolvedServerID(t *testing.T) {
	content := []json.RawMessage{
		json.RawMessage(`{"type":"tool_use","id":"toolu_fallback","name":"oma_web_search","input":{"query":"query"}}`),
	}
	executions := []webSearchExecution{{
		call: webSearchToolCall{id: "toolu_fallback", name: upstreamSearchToolName},
		results: websearch.SearchResponse{Results: []websearch.Result{{
			Title: "Result", URL: "https://example.com", Snippet: "snippet",
		}}},
	}}

	projected, err := projectCompletedWebSearchContent(content, executions)
	if err != nil {
		t.Fatalf("project completed content: %v", err)
	}
	if len(projected) != 2 {
		t.Fatalf("projected blocks = %d, want 2", len(projected))
	}
	var serverUse, searchResult webSearchProtocolBlock
	if err := json.Unmarshal(projected[0], &serverUse); err != nil {
		t.Fatalf("decode server tool use: %v", err)
	}
	if err := json.Unmarshal(projected[1], &searchResult); err != nil {
		t.Fatalf("decode search result: %v", err)
	}
	expectedID, err := serverWebSearchToolUseID("toolu_fallback")
	if err != nil {
		t.Fatalf("mint server tool use ID: %v", err)
	}
	if serverUse.ID != expectedID || searchResult.ToolUseID != expectedID {
		t.Fatalf("server use ID = %q, result ID = %q; want %q", serverUse.ID, searchResult.ToolUseID, expectedID)
	}
}

func TestWebSearchGatewayEnforcesCallerMaxUses(t *testing.T) {
	upstream, _ := newSequencedUpstream(t,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"first"}},{"type":"tool_use","id":"toolu_2","name":"oma_web_search","input":{"query":"second"}}],"stop_reason":"tool_use"}`,
		`{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305","max_uses":1}]}`)
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(searcher.requests) != 1 || searcher.requests[0].Query != "first" {
		t.Fatalf("provider requests = %#v, want only the first search", searcher.requests)
	}
	var decoded struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode max_uses response: %v", err)
	}
	if len(decoded.Content) != 5 {
		t.Fatalf("max_uses content = %s", response.body)
	}
	for _, index := range []int{0, 2} {
		var call webSearchContentBlock
		if err := json.Unmarshal(decoded.Content[index], &call); err != nil {
			t.Fatalf("decode server call %d: %v", index, err)
		}
		if call.Type != "server_tool_use" || !strings.HasPrefix(call.ID, "srvtoolu_") {
			t.Fatalf("server call %d = %s", index, decoded.Content[index])
		}
		var result webSearchProtocolBlock
		if err := json.Unmarshal(decoded.Content[index+1], &result); err != nil {
			t.Fatalf("decode server result %d: %v", index+1, err)
		}
		if result.ToolUseID != call.ID {
			t.Fatalf("server call/result ids = %q/%q", call.ID, result.ToolUseID)
		}
	}
	if !strings.Contains(string(decoded.Content[3]), `"error_code":"max_uses_exceeded"`) {
		t.Fatalf("max_uses error = %s", decoded.Content[3])
	}
}

func TestWebSearchGatewayMaxUsesSpansInternalContinuations(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"first"}}],"stop_reason":"tool_use"}`,
		`{"type":"message","content":[{"type":"tool_use","id":"toolu_2","name":"oma_web_search","input":{"query":"second"}}],"stop_reason":"tool_use"}`,
		`{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
	)
	searcher := &webSearchTestSearcher{}
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305","max_uses":1}]}`)
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if requestCount() != 3 {
		t.Fatalf("BYOK requests = %d, want 3", requestCount())
	}
	if len(searcher.requests) != 1 || searcher.requests[0].Query != "first" {
		t.Fatalf("provider requests = %#v, want only the first search", searcher.requests)
	}
	var decoded struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(response.body, &decoded); err != nil {
		t.Fatalf("decode max_uses continuation response: %v", err)
	}
	if len(decoded.Content) != 5 || !strings.Contains(string(decoded.Content[3]), `"error_code":"max_uses_exceeded"`) {
		t.Fatalf("max_uses continuation response = %s", response.body)
	}
}

// max_uses 是 per-request 上限（Anthropic: "limits the number of searches performed"
// per request）。pause_turn 续传是新的入站请求，因此按官方语义重新获得完整额度；本测试
// 把该语义钉住，避免有人误以为额度应该跨续传累计。
func TestWebSearchGatewayMaxUsesResetsPerInboundRequest(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_pause","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"first"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_second","type":"message","content":[{"type":"tool_use","id":"toolu_2","name":"oma_web_search","input":{"query":"second"}}],"stop_reason":"tool_use"}`,
	)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.test"}}}}
	cfg := config.Config{
		AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"},
		WebSearch:         config.WebSearchConfig{MaxServerToolIterations: 1},
	}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	tools := `[{"type":"web_search_20250305","name":"web_search","max_uses":1}]`

	paused, handled, err := webSearchGateway.handle(context.Background(),
		[]byte(`{"model":"model","max_tokens":32,"messages":[{"role":"user","content":"search"}],"tools":`+tools+`}`), "", nil)
	if err != nil || !handled {
		t.Fatalf("paused response handled = %v, err = %v", handled, err)
	}
	var pausedBody struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
	}
	if err := json.Unmarshal(paused.body, &pausedBody); err != nil {
		t.Fatalf("decode paused response: %v", err)
	}
	if pausedBody.StopReason != "pause_turn" || len(searcher.requests) != 1 {
		t.Fatalf("paused stop_reason = %q, searches = %d; want pause_turn after one search", pausedBody.StopReason, len(searcher.requests))
	}

	pausedAssistant, err := json.Marshal(map[string]any{"role": "assistant", "content": pausedBody.Content})
	if err != nil {
		t.Fatalf("encode paused assistant message: %v", err)
	}
	continuation, handled, err := webSearchGateway.handle(context.Background(),
		[]byte(`{"model":"model","max_tokens":32,"messages":[{"role":"user","content":"search"},`+
			string(pausedAssistant)+`],"tools":`+tools+`}`), "", nil)
	if err != nil || !handled || continuation.statusCode != http.StatusOK {
		t.Fatalf("continuation = %#v, handled = %v, err = %v", continuation, handled, err)
	}
	if requestCount() != 2 {
		t.Fatalf("BYOK requests = %d, want 2", requestCount())
	}
	if len(searcher.requests) != 2 {
		t.Fatalf("provider searches = %d, want a fresh per-request max_uses budget on the continuation", len(searcher.requests))
	}
	if strings.Contains(string(continuation.body), `"error_code":"max_uses_exceeded"`) {
		t.Fatalf("continuation response = %s, want no max_uses error on a new inbound request", continuation.body)
	}
}

// Anthropic 用 usage.server_tool_use.web_search_requests 上报搜索次数，且规定失败的搜索
// 不计费；BYOK 只看到 ordinary tool，不会上报该字段，因此由 gateway 补齐。
func TestWebSearchGatewaySynthesizesServerToolUsage(t *testing.T) {
	t.Run("failure provider error is not billed", func(t *testing.T) {
		upstream, _ := newSequencedUpstream(t,
			`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"query"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
			`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":8}}`,
		)
		searcher := &webSearchTestSearcher{err: errors.New("provider unavailable")}
		cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
		webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
		body := []byte(`{"model":"model","max_tokens":32,"messages":[],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)

		response, _, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if strings.Contains(string(response.body), "web_search_requests") {
			t.Fatalf("usage = %s, want no billable search after a provider error", response.body)
		}
	})

	t.Run("failure max_uses_exceeded is not billed", func(t *testing.T) {
		upstream, _ := newSequencedUpstream(t,
			`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"first"}},{"type":"tool_use","id":"toolu_2","name":"oma_web_search","input":{"query":"second"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
			`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":8}}`,
		)
		searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.test"}}}}
		cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
		webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
		body := []byte(`{"model":"model","max_tokens":32,"messages":[],"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":1}]}`)

		response, _, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if !strings.Contains(string(response.body), `"web_search_requests":1`) {
			t.Fatalf("usage = %s, want only the executed search billed", response.body)
		}
	})

	t.Run("success counts every executed search", func(t *testing.T) {
		upstream, _ := newSequencedUpstream(t,
			`{"id":"msg_1","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"oma_web_search","input":{"query":"first"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`,
			`{"id":"msg_2","type":"message","content":[{"type":"tool_use","id":"toolu_2","name":"oma_web_search","input":{"query":"second"}}],"stop_reason":"tool_use","usage":{"input_tokens":20,"output_tokens":8}}`,
			`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":30,"output_tokens":12,"server_tool_use":{"web_fetch_requests":3}}}`,
		)
		searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Result", URL: "https://example.test"}}}}
		cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
		webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
		body := []byte(`{"model":"model","max_tokens":32,"messages":[],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)

		response, _, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		var decoded struct {
			Usage struct {
				InputTokens   int64 `json:"input_tokens"`
				OutputTokens  int64 `json:"output_tokens"`
				ServerToolUse struct {
					WebSearchRequests int `json:"web_search_requests"`
					WebFetchRequests  int `json:"web_fetch_requests"`
				} `json:"server_tool_use"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(response.body, &decoded); err != nil {
			t.Fatalf("decode usage: %v", err)
		}
		if decoded.Usage.ServerToolUse.WebSearchRequests != 2 {
			t.Fatalf("web_search_requests = %d, want 2", decoded.Usage.ServerToolUse.WebSearchRequests)
		}
		if decoded.Usage.ServerToolUse.WebFetchRequests != 3 {
			t.Fatalf("web_fetch_requests = %d, want the upstream server tool usage preserved", decoded.Usage.ServerToolUse.WebFetchRequests)
		}
		if decoded.Usage.InputTokens != 60 || decoded.Usage.OutputTokens != 25 {
			t.Fatalf("token usage = %#v, want accumulated 60/25", decoded.Usage)
		}
	})

	t.Run("success no search leaves upstream usage untouched", func(t *testing.T) {
		upstream, _ := newSequencedUpstream(t,
			`{"id":"msg_final","type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":7}}`,
		)
		cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
		webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, &webSearchTestSearcher{}, nil)
		body := []byte(`{"model":"model","max_tokens":32,"messages":[],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)

		response, _, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
		if err != nil {
			t.Fatalf("handle: %v", err)
		}
		if !strings.Contains(string(response.body), `"usage":{"input_tokens":11,"output_tokens":7}`) {
			t.Fatalf("usage = %s, want the upstream usage passed through unchanged", response.body)
		}
	})
}

func TestWebSearchGatewayRejectsUnsupportedCallerLocation(t *testing.T) {
	requestCount := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	defer upstream.Close()
	cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, &webSearchTestSearcher{}, nil)
	body := []byte(`{"messages":[],"tools":[{"type":"web_search_20250305","user_location":{"type":"approximate","country":"US"}}]}`)
	_, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
	if !handled || err == nil || !strings.Contains(err.Error(), "user_location is unsupported") {
		t.Fatalf("handled = %v, err = %v, want explicit unsupported user_location error", handled, err)
	}
	if requestCount != 0 {
		t.Fatalf("upstream requests = %d, want 0", requestCount)
	}
}

func TestWebSearchGatewayRejectsUnsupportedSearchCallersBeforeUpstream(t *testing.T) {
	for _, test := range []struct {
		name string
		tool string
	}{
		{name: "legacy explicit code execution", tool: `{"type":"web_search_20250305","allowed_callers":["code_execution_20260120"]}`},
		{name: "dynamic filtering default", tool: `{"type":"web_search_20260209"}`},
		{name: "latest dynamic filtering default", tool: `{"type":"web_search_20260318"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requestCount atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requestCount.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"type":"message","content":[{"type":"text","text":"unexpected"}],"stop_reason":"end_turn"}`)
			}))
			defer upstream.Close()
			cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
			webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, &webSearchTestSearcher{}, nil)
			body := []byte(`{"messages":[],"tools":[` + test.tool + `]}`)
			_, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
			if !handled || err == nil || !strings.Contains(err.Error(), `allowed_callers must include "direct"`) {
				t.Fatalf("handled = %v, err = %v; want direct-caller validation error", handled, err)
			}
			if requestCount.Load() != 0 {
				t.Fatalf("upstream requests = %d, want 0", requestCount.Load())
			}
		})
	}
}

func TestWebSearchGatewayRejectsInvalidResponseInclusionBeforeUpstream(t *testing.T) {
	for _, test := range []struct {
		name string
		tool string
	}{
		{name: "unsupported version", tool: `{"type":"web_search_20260209","allowed_callers":["direct"],"response_inclusion":"excluded"}`},
		{name: "unsupported value", tool: `{"type":"web_search_20260318","allowed_callers":["direct"],"response_inclusion":"summary"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requestCount atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requestCount.Add(1)
			}))
			defer upstream.Close()
			cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
			webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, &webSearchTestSearcher{}, nil)
			body := []byte(`{"messages":[],"tools":[` + test.tool + `]}`)
			_, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
			if !handled || err == nil || !strings.Contains(err.Error(), "response_inclusion") {
				t.Fatalf("handled = %v, err = %v; want response_inclusion validation error", handled, err)
			}
			if requestCount.Load() != 0 {
				t.Fatalf("upstream requests = %d, want 0", requestCount.Load())
			}
		})
	}
}

func TestWebSearchGatewayAcceptsDirectCallerForDynamicSearchVersion(t *testing.T) {
	for _, test := range []struct {
		name string
		tool string
	}{
		{name: "dynamic version", tool: `{"type":"web_search_20260209","allowed_callers":["direct"]}`},
		{name: "latest direct exclusion is still full", tool: `{"type":"web_search_20260318","allowed_callers":["direct"],"response_inclusion":"excluded"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream, requestCount := newSequencedUpstream(t, `{"type":"message","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
			cfg := config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}}
			webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, &webSearchTestSearcher{}, nil)
			body := []byte(`{"messages":[],"tools":[` + test.tool + `]}`)
			response, handled, err := webSearchGateway.handle(context.Background(), body, "", nil)
			if err != nil || !handled || response.statusCode != http.StatusOK || requestCount() != 1 {
				t.Fatalf("response = %#v, handled = %v, err = %v, requests = %d", response, handled, err, requestCount())
			}
		})
	}
}

func TestWebSearchGatewayToolLoopProjectsTranscript(t *testing.T) {
	var requests []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		if len(requests) == 1 {
			_, _ = io.WriteString(w, "{\"id\":\"msg_tool\",\"type\":\"message\",\"content\":[{\"type\":\"text\",\"text\":\"looking\"},{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"oma_web_search\",\"input\":{\"query\":\"golang release\"}}],\"stop_reason\":\"tool_use\"}")
			return
		}
		_, _ = io.WriteString(w, "{\"id\":\"msg_final\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"answer\"}],\"stop_reason\":\"end_turn\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}")
	}))
	defer upstream.Close()
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Go", URL: "https://go.dev", Snippet: "release"}}}}
	cfg := config.Config{
		AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "upstream-key"},
		WebSearch: config.WebSearchConfig{
			Provider:                "tavily",
			MaxServerToolIterations: 2,
			Providers: map[string]config.WebSearchProviderConfig{
				"tavily": {APIKey: "tavily-key"},
			},
		},
	}
	webSearchGateway := newWebSearchGateway(cfg, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"messages\":[{\"role\":\"user\",\"content\":\"search\"}],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\",\"max_uses\":1,\"allowed_domains\":[\"go.dev\"]}],\"tool_choice\":{\"type\":\"tool\",\"name\":\"web_search\"}}")
	response, handled, err := webSearchGateway.handle(context.Background(), body, "beta=true", http.Header{"Anthropic-Version": []string{"2023-06-01"}})
	if err != nil || !handled || response.statusCode != http.StatusOK {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if len(requests) != 2 || len(searcher.queries) != 1 || searcher.queries[0] != "golang release" {
		t.Fatalf("requests = %d, queries = %#v", len(requests), searcher.queries)
	}
	if len(searcher.requests) != 1 || searcher.requests[0].Options.MaxResults != 5 {
		t.Fatalf("search requests = %#v", searcher.requests)
	}
	if len(searcher.requests[0].Options.IncludeDomains) != 1 || searcher.requests[0].Options.IncludeDomains[0] != "go.dev" {
		t.Fatalf("search domain options = %#v", searcher.requests[0].Options)
	}
	encodedFirstRequest, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatalf("marshal first request: %v", err)
	}
	if strings.Contains(string(encodedFirstRequest), "tavily-key") {
		t.Fatal("Tavily API key reached the BYOK request")
	}
	tools, ok := requests[0]["tools"].([]any)
	if !ok || len(tools) != 1 || tools[0].(map[string]any)["name"] != upstreamSearchToolName {
		t.Fatalf("projected tools = %#v", requests[0]["tools"])
	}
	toolChoice, ok := requests[0]["tool_choice"].(map[string]any)
	if !ok || toolChoice["type"] != "tool" || toolChoice["name"] != upstreamSearchToolName {
		t.Fatalf("projected tool_choice = %#v", requests[0]["tool_choice"])
	}
	if strings.Contains(string(encodedFirstRequest), "allowed_domains") || strings.Contains(string(encodedFirstRequest), "max_uses") {
		t.Fatalf("caller search policy leaked into model-controlled tool input: %s", encodedFirstRequest)
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
	if len(content) != 4 || content[0].(map[string]any)["text"] != "looking" ||
		content[1].(map[string]any)["type"] != "server_tool_use" ||
		content[2].(map[string]any)["type"] != "web_search_tool_result" ||
		content[3].(map[string]any)["text"] != "answer" {
		t.Fatalf("projected content = %#v", content)
	}
}

func TestWebSearchGatewaySSEResponse(t *testing.T) {
	upstream, requestCount := newSequencedUpstream(t,
		`{"id":"msg_tool","type":"message","content":[{"type":"tool_use","id":"toolu_sse","name":"oma_web_search","input":{"query":"golang release"}}],"stop_reason":"tool_use"}`,
		`{"id":"msg_final","type":"message","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{}}`,
	)
	searcher := &webSearchTestSearcher{results: websearch.SearchResponse{Results: []websearch.Result{{Title: "Go", URL: "https://go.dev", Snippet: "release"}}}}
	webSearchGateway := newWebSearchGateway(config.Config{AnthropicUpstream: config.AnthropicUpstreamConfig{BaseURL: upstream.URL, APIKey: "key"}, WebSearch: config.WebSearchConfig{MaxServerToolIterations: 2}}, &http.Client{Timeout: time.Second}, searcher, nil)
	body := []byte("{\"model\":\"model\",\"max_tokens\":32,\"stream\":true,\"messages\":[],\"tools\":[{\"type\":\"web_search_20250305\",\"name\":\"web_search\"}]}")
	response, handled, err := webSearchGateway.handle(context.Background(), body, "", http.Header{})
	if err != nil || !handled || !strings.Contains(string(response.body), "event: message_start") || !strings.Contains(string(response.body), "event: message_stop") {
		t.Fatalf("response = %#v, handled = %v, err = %v", response, handled, err)
	}
	if got := response.header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := response.header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if got := response.header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
	if requestCount() != 2 || len(searcher.requests) != 1 {
		t.Fatalf("continuation requests = %d, searches = %d; want 2 and 1", requestCount(), len(searcher.requests))
	}
	for _, want := range []string{"server_tool_use", "web_search_tool_result", "web_search_result", "event: content_block_start"} {
		if !strings.Contains(string(response.body), want) {
			t.Fatalf("SSE response = %s, missing %q", response.body, want)
		}
	}
	if !strings.Contains(string(response.body), `"type":"input_json_delta"`) || !strings.Contains(string(response.body), `"partial_json":"{\"query\":\"golang release\"}"`) {
		t.Fatalf("SSE response missing server tool input delta: %s", response.body)
	}
}
