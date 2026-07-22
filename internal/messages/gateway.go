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
	id     string
	name   string
	input  json.RawMessage
	search *gatewaySearchInput
}

type gatewayExecution struct {
	call    gatewayToolCall
	results websearch.SearchResponse
	err     error
}

type gatewaySearchInput struct {
	Query          string               `json:"query"`
	MaxUses        int                  `json:"max_uses,omitempty"`
	AllowedDomains []string             `json:"allowed_domains,omitempty"`
	BlockedDomains []string             `json:"blocked_domains,omitempty"`
	UserLocation   *gatewayUserLocation `json:"user_location,omitempty"`
}

type gatewayUserLocation struct {
	Type     string `json:"type"`
	City     string `json:"city,omitempty"`
	Region   string `json:"region,omitempty"`
	Country  string `json:"country,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

type gatewayContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	Text  string          `json:"text,omitempty"`
}

type gatewayToolResultBlock struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content"`
}

type gatewaySearchResultBlock struct {
	Type          string `json:"type,omitempty"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	Content       string `json:"content"`
	PublishedDate string `json:"published_date,omitempty"`
	PageAge       string `json:"page_age,omitempty"`
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
		return gatewayResponse{}, true, errors.New("messages web search gateway is not configured")
	}
	if int64(len(body)) > maxRequestBodyBytes {
		return gatewayResponse{}, true, errors.New("request body exceeds maximum size")
	}
	if g.searcher == nil {
		return gatewayResponse{}, false, nil
	}
	if g.client == nil {
		return gatewayResponse{}, true, errors.New("messages upstream client is not configured")
	}
	request, err := parseGatewayRequest(body)
	if err != nil {
		return gatewayResponse{}, false, nil
	}
	if _, ok := request.fields["tools"]; !ok || !hasWebSearchTool(request.fields["tools"]) {
		return gatewayResponse{}, false, nil
	}
	if strings.TrimSpace(g.upstreamAPIKey) == "" {
		return gatewayResponse{}, true, errors.New("messages upstream key is required")
	}
	upstreamFields, err := projectGatewayFields(request.fields)
	if err != nil {
		return gatewayResponse{}, true, fmt.Errorf("project messages request: %w", err)
	}
	transcript := append([]json.RawMessage{}, request.messages...)
	executions := []gatewayExecution{}
	searchUses := 0
	for loop := 0; loop < gatewayLoopLimit(g); loop++ {
		encodedMessages, err := json.Marshal(transcript)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode messages transcript: %w", err)
		}
		upstreamFields["messages"] = encodedMessages
		upstreamFields["stream"] = json.RawMessage("false")
		payload, err := json.Marshal(upstreamFields)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode upstream messages request: %w", err)
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
			return gatewayResponse{}, true, errors.New("messages upstream returned a non-JSON response")
		}
		calls, err := extractGatewayToolCalls(response.body)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("decode upstream messages response: %w", err)
		}
		if len(calls) == 0 {
			response.body, err = projectGatewayResponse(response.body, executions)
			if err != nil {
				return gatewayResponse{}, true, fmt.Errorf("project messages response: %w", err)
			}
			if request.stream {
				response.body, err = encodeGatewaySSE(response.body)
				if err != nil {
					return gatewayResponse{}, true, fmt.Errorf("encode messages stream: %w", err)
				}
				response.header.Set("Content-Type", "text/event-stream")
				prepareResponseHeaders(response.header)
			}
			response.header.Del("Content-Length")
			return response, true, nil
		}
		assistantMessage, err := assistantGatewayMessage(response.body)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode assistant messages transcript: %w", err)
		}
		transcript = append(transcript, assistantMessage)
		results, newExecutions, nextSearchUses, err := g.executeToolCalls(ctx, calls, searchUses)
		if err != nil {
			return gatewayResponse{}, true, err
		}
		executions = append(executions, newExecutions...)
		searchUses = nextSearchUses
		userMessage, err := userGatewayMessage(results)
		if err != nil {
			return gatewayResponse{}, true, fmt.Errorf("encode user messages transcript: %w", err)
		}
		transcript = append(transcript, userMessage)
	}
	return gatewayResponse{}, true, errors.New("web search tool loop exceeded maximum iterations")
}

func (g *gateway) executeToolCalls(ctx context.Context, calls []gatewayToolCall, searchUses int) ([]json.RawMessage, []gatewayExecution, int, error) {
	results := make([]json.RawMessage, 0, len(calls))
	executions := make([]gatewayExecution, 0, len(calls))
	for _, call := range calls {
		if call.search == nil {
			result, err := unsupportedToolResult(call)
			if err != nil {
				return nil, nil, searchUses, fmt.Errorf("encode unsupported tool result: %w", err)
			}
			results = append(results, result)
			continue
		}
		var searchErr error
		if call.search.UserLocation != nil {
			searchErr = errors.New("web search user_location is unsupported")
		} else if call.search.MaxUses > 0 && searchUses >= call.search.MaxUses {
			searchErr = errors.New("web search max uses exceeded")
		} else {
			searchUses++
		}
		result, searchErr := g.search(ctx, *call.search, searchErr)
		executions = append(executions, gatewayExecution{call: call, results: result, err: searchErr})
		toolResult, err := gatewayToolResult(call, result.Results, searchErr)
		if err != nil {
			return nil, nil, searchUses, fmt.Errorf("encode web search tool result: %w", err)
		}
		results = append(results, toolResult)
	}
	return results, executions, searchUses, nil
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
	var tools []json.RawMessage
	if json.Unmarshal(raw, &tools) != nil {
		return false
	}
	for _, rawTool := range tools {
		var tool struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			continue
		}
		if isWebSearchToolType(tool.Type) {
			return true
		}
	}
	return false
}

func projectGatewayFields(fields map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	projected := cloneRawMap(fields)
	var tools []json.RawMessage
	if err := json.Unmarshal(fields["tools"], &tools); err != nil {
		return nil, fmt.Errorf("tools must be an array: %w", err)
	}
	projectedTools := make([]json.RawMessage, 0, len(tools))
	for _, rawTool := range tools {
		var tool struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawTool, &tool); err != nil {
			return nil, fmt.Errorf("decode tool: %w", err)
		}
		if isWebSearchToolType(tool.Type) {
			projectedTools = append(projectedTools, searchToolDefinition())
			continue
		}
		projectedTools = append(projectedTools, rawTool)
	}
	encodedTools, err := json.Marshal(projectedTools)
	if err != nil {
		return nil, fmt.Errorf("encode tools: %w", err)
	}
	projected["tools"] = encodedTools
	return projected, nil
}

func isServerWebSearchToolType(value string) bool {
	switch value {
	case "web_search_20250305", "web_search_20260209", "web_search_20260318":
		return true
	default:
		return false
	}
}

func isWebSearchToolType(value string) bool {
	return value == searchToolName || isServerWebSearchToolType(value)
}

func searchToolDefinition() json.RawMessage {
	return json.RawMessage(`{"name":"web_search","description":"Search the public web and return relevant results.","input_schema":{"type":"object","properties":{"query":{"type":"string","description":"The web search query"},"max_uses":{"type":"integer","minimum":1},"allowed_domains":{"type":"array","items":{"type":"string"}},"blocked_domains":{"type":"array","items":{"type":"string"}},"user_location":{"type":"object","properties":{"type":{"type":"string","enum":["approximate"]},"city":{"type":"string"},"region":{"type":"string"},"country":{"type":"string"},"timezone":{"type":"string"}},"required":["type"]}},"required":["query"]}}`)
}

func (g *gateway) send(ctx context.Context, body []byte, rawQuery string, headers http.Header) (gatewayResponse, error) {
	target, err := messagesEndpoint(g.upstreamBaseURL, rawQuery)
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("build messages upstream endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("build messages upstream request: %w", err)
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
		return gatewayResponse{}, fmt.Errorf("send messages upstream request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGatewayResponseBytes+1))
	if err != nil {
		return gatewayResponse{}, fmt.Errorf("read messages upstream response: %w", err)
	}
	if len(responseBody) > maxGatewayResponseBytes {
		return gatewayResponse{}, errors.New("messages upstream response exceeds maximum size")
	}
	return gatewayResponse{statusCode: response.StatusCode, header: response.Header.Clone(), body: responseBody}, nil
}

func gatewayLoopLimit(g *gateway) int {
	if g.maxToolLoops > 0 {
		return g.maxToolLoops
	}
	return defaultGatewayLoops
}

func (g *gateway) search(ctx context.Context, input gatewaySearchInput, priorErr error) (results websearch.SearchResponse, err error) {
	if priorErr != nil {
		return results, priorErr
	}
	defer func() {
		if recover() != nil {
			results = websearch.SearchResponse{}
			err = errors.New("web search provider panicked")
		}
	}()
	return g.searcher.Search(ctx, websearch.SearchRequest{
		Query: input.Query,
		Options: websearch.SearchOptions{
			MaxResults:     5,
			IncludeDomains: append([]string(nil), input.AllowedDomains...),
			ExcludeDomains: append([]string(nil), input.BlockedDomains...),
		},
	})
}

func extractGatewayToolCalls(body []byte) ([]gatewayToolCall, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Content == nil {
		return nil, errors.New("messages response content must be an array")
	}
	calls := []gatewayToolCall{}
	for _, rawBlock := range response.Content {
		var block gatewayContentBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode messages content block: %w", err)
		}
		if block.Type != "tool_use" {
			continue
		}
		if strings.TrimSpace(block.ID) == "" {
			return nil, errors.New("tool use id is required")
		}
		call := gatewayToolCall{id: block.ID, name: block.Name, input: append(json.RawMessage(nil), block.Input...)}
		if block.Name != searchToolName {
			calls = append(calls, call)
			continue
		}
		var input gatewaySearchInput
		if len(block.Input) == 0 || json.Unmarshal(block.Input, &input) != nil {
			return nil, errors.New("web search tool input must be an object")
		}
		input.Query = strings.TrimSpace(input.Query)
		if input.Query == "" {
			return nil, errors.New("web search tool input query is required")
		}
		if len(input.AllowedDomains) > 0 && len(input.BlockedDomains) > 0 {
			return nil, errors.New("web search tool input cannot include both allowed_domains and blocked_domains")
		}
		call.search = &input
		calls = append(calls, call)
	}
	return calls, nil
}

func assistantGatewayMessage(body []byte) (json.RawMessage, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode assistant message: %w", err)
	}
	message := struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: "assistant", Content: response.Content}
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal assistant message: %w", err)
	}
	return encoded, nil
}

func userGatewayMessage(results []json.RawMessage) (json.RawMessage, error) {
	message := struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: "user", Content: results}
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal user message: %w", err)
	}
	return encoded, nil
}

func gatewayToolResult(call gatewayToolCall, results []websearch.Result, searchErr error) (json.RawMessage, error) {
	if searchErr != nil {
		return marshalGatewayToolResult(gatewayToolResultBlock{
			Type: "tool_result", ToolUseID: call.id, IsError: true,
			Content: json.RawMessage(`"web search unavailable"`),
		})
	}
	content := make([]gatewaySearchResultBlock, 0, len(results))
	for _, result := range results {
		content = append(content, resultToContentItem(result, ""))
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshal web search results: %w", err)
	}
	return marshalGatewayToolResult(gatewayToolResultBlock{Type: "tool_result", ToolUseID: call.id, Content: encoded})
}

func unsupportedToolResult(call gatewayToolCall) (json.RawMessage, error) {
	return marshalGatewayToolResult(gatewayToolResultBlock{
		Type: "tool_result", ToolUseID: call.id, IsError: true,
		Content: json.RawMessage(`"unsupported tool use"`),
	})
}

func marshalGatewayToolResult(result gatewayToolResultBlock) (json.RawMessage, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal tool result: %w", err)
	}
	return encoded, nil
}

func resultToContentItem(result websearch.Result, blockType string) gatewaySearchResultBlock {
	content := result.Snippet
	if result.Text != "" {
		content = result.Text
	}
	return gatewaySearchResultBlock{
		Type:          blockType,
		Title:         result.Title,
		URL:           result.URL,
		Content:       content,
		PublishedDate: result.PublishedDate,
		PageAge:       result.PageAge,
	}
}

func projectGatewayResponse(body []byte, executions []gatewayExecution) ([]byte, error) {
	if len(executions) == 0 {
		return body, nil
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	var content []json.RawMessage
	if err := json.Unmarshal(response["content"], &content); err != nil {
		return nil, errors.New("messages response content must be an array")
	}
	projected := make([]json.RawMessage, 0, len(content)+len(executions)*2)
	for _, execution := range executions {
		input := execution.call.input
		if len(input) == 0 {
			var err error
			input, err = json.Marshal(execution.call.search)
			if err != nil {
				return nil, fmt.Errorf("marshal web search input: %w", err)
			}
		}
		toolUse, err := json.Marshal(struct {
			Type  string          `json:"type"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}{Type: "server_tool_use", ID: execution.call.id, Name: searchToolName, Input: input})
		if err != nil {
			return nil, fmt.Errorf("marshal server tool use: %w", err)
		}
		projected = append(projected, toolUse)
		if execution.err != nil {
			result, err := json.Marshal(struct {
				Type      string `json:"type"`
				ToolUseID string `json:"tool_use_id"`
				Content   struct {
					Type      string `json:"type"`
					ErrorCode string `json:"error_code"`
				} `json:"content"`
			}{Type: "web_search_tool_result", ToolUseID: execution.call.id, Content: struct {
				Type      string `json:"type"`
				ErrorCode string `json:"error_code"`
			}{Type: "web_search_tool_result_error", ErrorCode: "unavailable"}})
			if err != nil {
				return nil, fmt.Errorf("marshal web search error: %w", err)
			}
			projected = append(projected, result)
			continue
		}
		resultContent := make([]gatewaySearchResultBlock, 0, len(execution.results.Results))
		for _, result := range execution.results.Results {
			resultContent = append(resultContent, resultToContentItem(result, "web_search_result"))
		}
		encodedContent, err := json.Marshal(resultContent)
		if err != nil {
			return nil, fmt.Errorf("marshal web search content: %w", err)
		}
		result, err := json.Marshal(struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
		}{Type: "web_search_tool_result", ToolUseID: execution.call.id, Content: encodedContent})
		if err != nil {
			return nil, fmt.Errorf("marshal web search result: %w", err)
		}
		projected = append(projected, result)
	}
	projected = append(projected, content...)
	encodedContent, err := json.Marshal(projected)
	if err != nil {
		return nil, fmt.Errorf("marshal projected messages content: %w", err)
	}
	response["content"] = encodedContent
	return json.Marshal(response)
}

func encodeGatewaySSE(body []byte) ([]byte, error) {
	var message map[string]json.RawMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return nil, err
	}
	var content []json.RawMessage
	if err := json.Unmarshal(message["content"], &content); err != nil {
		return nil, errors.New("messages response content must be an array")
	}
	messageStart := cloneRawMap(message)
	messageStart["content"] = json.RawMessage("[]")
	messageStart["stop_reason"] = json.RawMessage("null")
	messageStart["stop_sequence"] = json.RawMessage("null")
	var output bytes.Buffer
	if err := writeGatewaySSE(&output, "message_start", struct {
		Type    string                     `json:"type"`
		Message map[string]json.RawMessage `json:"message"`
	}{Type: "message_start", Message: messageStart}); err != nil {
		return nil, err
	}
	for index, rawBlock := range content {
		var block gatewayContentBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode messages content block: %w", err)
		}
		startBlock := append(json.RawMessage(nil), rawBlock...)
		if block.Type == "text" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rawBlock, &fields); err != nil {
				return nil, fmt.Errorf("decode text content block: %w", err)
			}
			fields["text"] = json.RawMessage(`""`)
			var err error
			startBlock, err = json.Marshal(fields)
			if err != nil {
				return nil, fmt.Errorf("marshal text content block: %w", err)
			}
		}
		if err := writeGatewaySSE(&output, "content_block_start", struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
		}{Type: "content_block_start", Index: index, ContentBlock: startBlock}); err != nil {
			return nil, err
		}
		if block.Text != "" {
			if err := writeGatewaySSE(&output, "content_block_delta", struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}{Type: "content_block_delta", Index: index, Delta: struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text_delta", Text: block.Text}}); err != nil {
				return nil, err
			}
		}
		if err := writeGatewaySSE(&output, "content_block_stop", struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}{Type: "content_block_stop", Index: index}); err != nil {
			return nil, err
		}
	}
	if err := writeGatewaySSE(&output, "message_delta", struct {
		Type  string `json:"type"`
		Delta struct {
			StopReason   json.RawMessage `json:"stop_reason"`
			StopSequence json.RawMessage `json:"stop_sequence"`
		} `json:"delta"`
		Usage json.RawMessage `json:"usage"`
	}{Type: "message_delta", Delta: struct {
		StopReason   json.RawMessage `json:"stop_reason"`
		StopSequence json.RawMessage `json:"stop_sequence"`
	}{StopReason: message["stop_reason"], StopSequence: message["stop_sequence"]}, Usage: message["usage"]}); err != nil {
		return nil, err
	}
	if err := writeGatewaySSE(&output, "message_stop", struct {
		Type string `json:"type"`
	}{Type: "message_stop"}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeGatewaySSE(output *bytes.Buffer, event string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", event, err)
	}
	output.WriteString("event: " + event + "\n")
	output.WriteString("data: ")
	output.Write(data)
	output.WriteString("\n\n")
	return nil
}

func cloneRawMap(source map[string]json.RawMessage) map[string]json.RawMessage {
	clone := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		clone[key] = append(json.RawMessage(nil), value...)
	}
	return clone
}
