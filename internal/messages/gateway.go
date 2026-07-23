package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

const (
	maxGatewayResponseBytes = 8 << 20
	searchToolName          = "web_search"
	defaultGatewayLoops     = 3
)

type gateway struct {
	upstreamBaseURL string
	upstreamAPIKey  string
	maxToolLoops    int
	client          *http.Client
	searcher        websearch.Provider
}

type gatewayResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

type gatewayRequest struct {
	fields   map[string]json.RawMessage
	messages []json.RawMessage
	stream   bool
}

type gatewayToolCall struct {
	id    string
	query string
}

type gatewayExecution struct {
	call    gatewayToolCall
	results []websearch.Result
	err     error
}

func newGateway(cfg config.Config, client *http.Client, searcher websearch.Provider) *gateway {
	if client == nil {
		client = &http.Client{Transport: newProxyTransport()}
	}
	return &gateway{
		upstreamBaseURL: cfg.AnthropicUpstream.BaseURL,
		upstreamAPIKey:  cfg.AnthropicUpstream.APIKey,
		maxToolLoops:    cfg.WebSearch.MaxToolLoops,
		client:          client,
		searcher:        searcher,
	}
}

func (g *gateway) handle(ctx context.Context, body []byte, rawQuery string, headers http.Header) (gatewayResponse, bool, error) {
	if g == nil {
		return gatewayResponse{}, true, errors.New("Messages web search gateway is not configured")
	}
	if int64(len(body)) > maxRequestBodyBytes {
		return gatewayResponse{}, true, errors.New("request body exceeds maximum size")
	}
	if g.searcher == nil {
		return gatewayResponse{}, false, nil
	}
	if g.client == nil {
		return gatewayResponse{}, true, errors.New("Messages upstream client is not configured")
	}
	request, err := parseGatewayRequest(body)
	if err != nil {
		return gatewayResponse{}, false, nil
	}
	if _, ok := request.fields["tools"]; !ok || !hasWebSearchTool(request.fields["tools"]) {
		return gatewayResponse{}, false, nil
	}
	if strings.TrimSpace(g.upstreamAPIKey) == "" {
		return gatewayResponse{}, true, errors.New("Messages upstream key is required")
	}
	upstreamFields, err := projectGatewayFields(request.fields)
	if err != nil {
		return gatewayResponse{}, true, fmt.Errorf("project Messages request: %w", err)
	}
	transcript := append([]json.RawMessage{}, request.messages...)
	executions := []gatewayExecution{}
	for loop := 0; loop < gatewayLoopLimit(g); loop++ {
		encodedMessages, err := json.Marshal(transcript)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode Messages transcript: %w", err)
		}
		upstreamFields["messages"] = encodedMessages
		upstreamFields["stream"] = json.RawMessage("false")
		payload, err := json.Marshal(upstreamFields)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode upstream Messages request: %w", err)
		}
		response, err := g.send(ctx, payload, rawQuery, headers)
		if err != nil {
			return gatewayResponse{}, true, err
		}
		if response.statusCode < http.StatusOK || response.statusCode >= http.StatusMultipleChoices {
			return response, true, nil
		}
		contentType := strings.ToLower(response.header.Get("Content-Type"))
		if contentType != "" && !strings.Contains(contentType, "application/json") {
			return gatewayResponse{}, true, errors.New("Messages upstream returned a non-JSON response")
		}
		calls, err := extractGatewayToolCalls(response.body)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("decode upstream Messages response: %w", err)
		}
		if len(calls) == 0 {
			response.body, err = projectGatewayResponse(response.body, executions)
			if err != nil {
				return gatewayResponse{}, true, fmt.Errorf("project Messages response: %w", err)
			}
			if request.stream {
				response.body, err = encodeGatewaySSE(response.body)
				if err != nil {
					return gatewayResponse{}, true, fmt.Errorf("encode Messages stream: %w", err)
				}
				response.header.Set("Content-Type", "text/event-stream")
				response.header.Set("Cache-Control", "no-cache")
				response.header.Set("X-Accel-Buffering", "no")
			}
			response.header.Del("Content-Length")
			return response, true, nil
		}
		transcript = append(transcript, assistantGatewayMessage(response.body))
		results := make([]map[string]any, 0, len(calls))
		for _, call := range calls {
			result, searchErr := g.searcher.Search(ctx, call.query, websearch.SearchOptions{MaxResults: 5})
			executions = append(executions, gatewayExecution{call: call, results: result, err: searchErr})
			results = append(results, gatewayToolResult(call.id, result, searchErr))
		}
		transcript = append(transcript, userGatewayMessage(results))
	}
	return gatewayResponse{}, true, errors.New("web search tool loop exceeded maximum iterations")
}

func parseGatewayRequest(body []byte) (gatewayRequest, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &fields); err != nil {
		return gatewayRequest{}, fmt.Errorf("invalid JSON request body: %w", err)
	}
	var messages []json.RawMessage
	if raw := fields["messages"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &messages); err != nil {
			return gatewayRequest{}, fmt.Errorf("messages must be an array: %w", err)
		}
	}
	var stream bool
	if raw := fields["stream"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &stream); err != nil {
			return gatewayRequest{}, fmt.Errorf("stream must be a boolean: %w", err)
		}
	}
	return gatewayRequest{fields: fields, messages: messages, stream: stream}, nil
}

func hasWebSearchTool(raw json.RawMessage) bool {
	var tools []map[string]json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return false
	}
	for _, tool := range tools {
		typ, _ := rawString(tool["type"])
		if strings.HasPrefix(typ, "web_search_") {
			return true
		}
	}
	return false
}

func projectGatewayFields(fields map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	projected := cloneRawMap(fields)
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(fields["tools"], &tools); err != nil {
		return nil, fmt.Errorf("tools must be an array: %w", err)
	}
	projectedTools := make([]map[string]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		typ, _ := rawString(tool["type"])
		if strings.HasPrefix(typ, "web_search_") {
			projectedTools = append(projectedTools, searchToolDefinition())
			continue
		}
		projectedTools = append(projectedTools, tool)
	}
	encodedTools, err := json.Marshal(projectedTools)
	if err != nil {
		return nil, fmt.Errorf("encode tools: %w", err)
	}
	projected["tools"] = encodedTools
	return projected, nil
}

func searchToolDefinition() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"name":         json.RawMessage("\"web_search\""),
		"description":  json.RawMessage("\"Search the public web and return relevant results.\""),
		"input_schema": json.RawMessage("{\"type\":\"object\",\"properties\":{\"query\":{\"type\":\"string\",\"description\":\"The web search query\"}},\"required\":[\"query\"]}"),
	}
}

func (g *gateway) send(ctx context.Context, body []byte, rawQuery string, headers http.Header) (gatewayResponse, error) {
	target, err := messagesEndpoint(g.upstreamBaseURL, rawQuery)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("build Messages upstream endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("build Messages upstream request: %w", err)
	}
	request.Header = sanitizedRequestHeaders(headers)
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("X-Api-Key", strings.TrimSpace(g.upstreamAPIKey))
	if request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := g.client.Do(request)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("send Messages upstream request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayResponseBytes+1))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("read Messages upstream response: %w", err)
	}
	if len(responseBody) > maxGatewayResponseBytes {
		return gatewayResponse{}, errors.New("Messages upstream response exceeds maximum size")
	}
	return gatewayResponse{statusCode: response.StatusCode, header: response.Header.Clone(), body: responseBody}, nil
}

func gatewayLoopLimit(g *gateway) int {
	if g.maxToolLoops > 0 {
		return g.maxToolLoops
	}
	return defaultGatewayLoops
}

func extractGatewayToolCalls(body []byte) ([]gatewayToolCall, error) {
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	content, ok := response["content"].([]any)
	if !ok {
		return nil, errors.New("Messages response content must be an array")
	}
	calls := []gatewayToolCall{}
	for _, value := range content {
		block, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("Messages content block must be an object")
		}
		if block["type"] != "tool_use" || block["name"] != searchToolName {
			continue
		}
		id, ok := block["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return nil, errors.New("web search tool use id is required")
		}
		input, ok := block["input"].(map[string]any)
		if !ok {
			return nil, errors.New("web search tool input must be an object")
		}
		query, ok := input["query"].(string)
		if !ok || strings.TrimSpace(query) == "" {
			return nil, errors.New("web search tool input query is required")
		}
		calls = append(calls, gatewayToolCall{id: id, query: strings.TrimSpace(query)})
	}
	return calls, nil
}

func assistantGatewayMessage(body []byte) json.RawMessage {
	var response map[string]any
	if json.Unmarshal(body, &response) != nil {
		return json.RawMessage("{\"role\":\"assistant\",\"content\":[]}")
	}
	message := map[string]any{"role": "assistant", "content": response["content"]}
	encoded, err := json.Marshal(message)
	if err != nil {
		return json.RawMessage("{\"role\":\"assistant\",\"content\":[]}")
	}
	return encoded
}

func userGatewayMessage(results []map[string]any) json.RawMessage {
	encoded, _ := json.Marshal(map[string]any{"role": "user", "content": results})
	return encoded
}

func gatewayToolResult(id string, results []websearch.Result, searchErr error) map[string]any {
	if searchErr != nil {
		return map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": true, "content": "web search unavailable"}
	}
	return map[string]any{"type": "tool_result", "tool_use_id": id, "content": results}
}

func projectGatewayResponse(body []byte, executions []gatewayExecution) ([]byte, error) {
	if len(executions) == 0 {
		return body, nil
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	content, ok := response["content"].([]any)
	if !ok {
		return nil, errors.New("Messages response content must be an array")
	}
	projected := make([]any, 0, len(content)+len(executions)*2)
	for _, execution := range executions {
		projected = append(projected, map[string]any{"type": "server_tool_use", "id": execution.call.id, "name": "web_search", "input": map[string]any{"query": execution.call.query}})
		if execution.err != nil {
			projected = append(projected, map[string]any{"type": "web_search_tool_result", "tool_use_id": execution.call.id, "content": map[string]any{"type": "web_search_tool_result_error", "error_code": "unavailable"}})
			continue
		}
		resultContent := make([]map[string]any, 0, len(execution.results))
		for _, result := range execution.results {
			item := map[string]any{"type": "web_search_result", "title": result.Title, "url": result.URL, "content": result.Content}
			if result.PublishedDate != "" {
				item["page_age"] = result.PublishedDate
			}
			resultContent = append(resultContent, item)
		}
		projected = append(projected, map[string]any{"type": "web_search_tool_result", "tool_use_id": execution.call.id, "content": resultContent})
	}
	projected = append(projected, content...)
	response["content"] = projected
	return json.Marshal(response)
}

func encodeGatewaySSE(body []byte) ([]byte, error) {
	var message map[string]any
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, err
	}
	content, ok := message["content"].([]any)
	if !ok {
		return nil, errors.New("Messages response content must be an array")
	}
	messageStart := cloneAnyMap(message)
	messageStart["content"] = []any{}
	messageStart["stop_reason"] = nil
	messageStart["stop_sequence"] = nil
	var output bytes.Buffer
	writeGatewaySSE(&output, "message_start", map[string]any{"type": "message_start", "message": messageStart})
	for index, value := range content {
		block, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("Messages content block must be an object")
		}
		startBlock := cloneAnyMap(block)
		if startBlock["type"] == "text" {
			startBlock["text"] = ""
		}
		writeGatewaySSE(&output, "content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": startBlock})
		if text, ok := block["text"].(string); ok && text != "" {
			writeGatewaySSE(&output, "content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": text}})
		}
		writeGatewaySSE(&output, "content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}
	writeGatewaySSE(&output, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": message["stop_reason"], "stop_sequence": message["stop_sequence"]}, "usage": message["usage"]})
	writeGatewaySSE(&output, "message_stop", map[string]any{"type": "message_stop"})
	return output.Bytes(), nil
}

func writeGatewaySSE(output *bytes.Buffer, event string, value map[string]any) {
	data, _ := json.Marshal(value)
	output.WriteString("event: " + event + "\n")
	output.WriteString("data: ")
	output.Write(data)
	output.WriteString("\n\n")
}

func cloneRawMap(source map[string]json.RawMessage) map[string]json.RawMessage {
	clone := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}

func cloneAnyMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func rawString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}
