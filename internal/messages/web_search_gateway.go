package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

const (
	maxWebSearchResponseBytes = 8 << 20
	// searchToolName 是 Anthropic server tool 的协议名，只出现在面向调用方的
	// server_tool_use / web_search_tool_result block 中。
	searchToolName = "web_search"
	// upstreamSearchToolName 是 gateway 投影给 BYOK 的 ordinary tool 名。使用 OMA
	// 独占前缀是因为调用方可以合法地声明自己的 web_search 工具，复用该名字会让同一
	// 请求出现两个同名 tool，且无法再按名字判断 tool_use 归属。
	upstreamSearchToolName      = "oma_web_search"
	defaultServerToolIterations = 10
	searchErrorUnavailable      = "unavailable"
	searchErrorMaxUses          = "max_uses_exceeded"
)

// webSearchGateway coordinates BYOK sampling and managed provider execution.
type webSearchGateway struct {
	upstreamBaseURL         string
	upstreamAPIKey          string
	maxServerToolIterations int
	client                  *http.Client
	searcher                websearch.Provider
	logger                  *slog.Logger
}

type webSearchGatewayRequestError struct {
	cause error
}

func (e *webSearchGatewayRequestError) Error() string {
	return e.cause.Error()
}

func (e *webSearchGatewayRequestError) Unwrap() error {
	return e.cause
}

type webSearchPreparedRequest struct {
	request          webSearchGatewayRequest
	upstreamFields   map[string]json.RawMessage
	transcript       []json.RawMessage
	projectedContent []json.RawMessage
	searchUses       int
	searchEnabled    bool
	searchPolicy     webSearchPolicy
}

type webSearchToolCall struct {
	id         string
	externalID string
	name       string
	input      json.RawMessage
	search     *webSearchInput
}

type webSearchExecution struct {
	call      webSearchToolCall
	results   websearch.SearchResponse
	err       error
	errorCode string
}

// serverToolUseID 返回该次执行面向调用方的 server tool ID。续传路径已带上调用方原始
// ID，首轮执行则从 BYOK 的 ordinary ID 铸造。
func (e webSearchExecution) serverToolUseID() (string, error) {
	if e.call.externalID != "" {
		return e.call.externalID, nil
	}
	return serverWebSearchToolUseID(e.call.id)
}

type webSearchInput struct {
	Query string `json:"query"`
}

func newWebSearchGateway(cfg config.Config, client *http.Client, searcher websearch.Provider, logger *slog.Logger) *webSearchGateway {
	if client == nil {
		client = &http.Client{Transport: newProxyTransport()}
	}
	logger = logging.LoggerOrDefault(logger)
	return &webSearchGateway{
		upstreamBaseURL:         cfg.AnthropicUpstream.BaseURL,
		upstreamAPIKey:          cfg.AnthropicUpstream.APIKey,
		maxServerToolIterations: cfg.WebSearch.MaxServerToolIterations,
		client:                  client,
		searcher:                searcher,
		logger:                  logger,
	}
}

func (g *webSearchGateway) handle(ctx context.Context, body []byte, rawQuery string, headers http.Header) (webSearchGatewayResponse, bool, error) {
	prepared, handled, err := g.prepareRequest(ctx, body)
	if err != nil || !handled {
		return webSearchGatewayResponse{}, handled, err
	}
	request := prepared.request
	upstreamFields := prepared.upstreamFields
	transcript := prepared.transcript
	projectedContent := prepared.projectedContent
	searchUses := prepared.searchUses
	searchEnabled := prepared.searchEnabled
	searchPolicy := prepared.searchPolicy
	iterationLimit := g.serverToolIterationLimit()
	usage := &webSearchUsageAccumulator{}
	// iterationLimit 恒为正数，循环由下面的 pause_turn 分支退出，因此不需要循环条件。
	for iteration := 0; ; iteration++ {
		encodedMessages, err := json.Marshal(transcript)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode messages transcript: %w", err)
		}
		upstreamFields["messages"] = encodedMessages
		upstreamFields["stream"] = json.RawMessage("false")
		payload, err := json.Marshal(upstreamFields)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode upstream messages request: %w", err)
		}
		response, err := g.send(ctx, payload, rawQuery, headers)
		if err != nil {
			return webSearchGatewayResponse{}, true, err
		}
		if response.statusCode < http.StatusOK || response.statusCode >= http.StatusMultipleChoices {
			return response, true, nil
		}
		contentType := strings.ToLower(response.header.Get("Content-Type"))
		if contentType != "" && !strings.Contains(contentType, "application/json") {
			return webSearchGatewayResponse{}, true, errors.New("messages upstream returned a non-JSON response")
		}
		calls, err := extractWebSearchToolCalls(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("decode upstream messages response: %w", err)
		}
		content, err := webSearchResponseContent(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("decode upstream messages content: %w", err)
		}
		if err := usage.add(response.body); err != nil {
			return webSearchGatewayResponse{}, true, err
		}
		searchCalls := webSearchCalls(calls)
		if !searchEnabled && len(searchCalls) > 0 {
			return webSearchGatewayResponse{}, true, errors.New("messages upstream requested web search without a current tool definition")
		}
		if len(searchCalls) == 0 {
			projectedContent = append(projectedContent, content...)
			response, err = finalizeWebSearchResponse(response, projectedContent, request.stream, "", usage)
			return response, true, err
		}
		if len(searchCalls) != len(calls) {
			mixedContent, projectErr := projectPendingWebSearchContent(content)
			if projectErr != nil {
				return webSearchGatewayResponse{}, true, fmt.Errorf("project mixed tool response: %w", projectErr)
			}
			projectedContent = append(projectedContent, mixedContent...)
			response, err = finalizeWebSearchResponse(response, projectedContent, request.stream, "", usage)
			return response, true, err
		}
		assistantMessage, err := webSearchAssistantMessage(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode assistant messages transcript: %w", err)
		}
		transcript = append(transcript, assistantMessage)
		results, executions, nextSearchUses, err := g.executeSearchCalls(ctx, searchCalls, searchPolicy, searchUses)
		if err != nil {
			return webSearchGatewayResponse{}, true, err
		}
		completedContent, err := projectCompletedWebSearchContent(content, executions)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("project web search response: %w", err)
		}
		projectedContent = append(projectedContent, completedContent...)
		searchUses = nextSearchUses
		if iteration+1 >= iterationLimit {
			response, err = finalizeWebSearchResponse(response, projectedContent, request.stream, "pause_turn", usage)
			return response, true, err
		}
		userMessage, err := webSearchUserMessage(results)
		if err != nil {
			return webSearchGatewayResponse{}, true, fmt.Errorf("encode user messages transcript: %w", err)
		}
		transcript = append(transcript, userMessage)
	}
}

func (g *webSearchGateway) prepareRequest(ctx context.Context, body []byte) (webSearchPreparedRequest, bool, error) {
	if g == nil {
		return webSearchPreparedRequest{}, true, errors.New("messages web search gateway is not configured")
	}
	if int64(len(body)) > maxRequestBodyBytes {
		return webSearchPreparedRequest{}, true, errors.New("request body exceeds maximum size")
	}
	if g.searcher == nil {
		return webSearchPreparedRequest{}, false, nil
	}
	if g.client == nil {
		return webSearchPreparedRequest{}, true, errors.New("messages upstream client is not configured")
	}
	request, err := parseWebSearchRequest(body)
	if err != nil {
		return webSearchPreparedRequest{}, false, nil
	}
	_, hasTools := request.fields["tools"]
	searchEnabled := hasTools && hasWebSearchTool(request.fields["tools"])
	if !searchEnabled && !hasWebSearchHistory(request.messages) {
		return webSearchPreparedRequest{}, false, nil
	}
	if !searchEnabled {
		if isWebSearchPauseContinuation(request.messages) {
			return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{
				cause: errors.New("pause_turn continuation requires the same web_search tool"),
			}
		}
		pending, pendingErr := findPendingWebSearchTurn(request.messages)
		if pendingErr != nil {
			return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{cause: pendingErr}
		}
		if pending != nil {
			return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{
				cause: errors.New("pending web search continuation requires the same web_search tool"),
			}
		}
	}
	if strings.TrimSpace(g.upstreamAPIKey) == "" {
		return webSearchPreparedRequest{}, true, errors.New("messages upstream key is required")
	}
	upstreamFields, searchPolicy, err := projectWebSearchFields(request.fields)
	if err != nil {
		return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{cause: fmt.Errorf("project messages request: %w", err)}
	}
	if searchEnabled {
		if err := g.searcher.ValidateOptions(webSearchOptions(searchPolicy)); err != nil {
			return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{cause: fmt.Errorf("validate web search provider options: %w", err)}
		}
	}
	transcript, projectedContent, searchUses, err := g.prepareWebSearchTranscript(ctx, request.messages, searchPolicy)
	if err != nil {
		return webSearchPreparedRequest{}, true, &webSearchGatewayRequestError{cause: fmt.Errorf("project messages transcript: %w", err)}
	}
	return webSearchPreparedRequest{
		request:          request,
		upstreamFields:   upstreamFields,
		transcript:       transcript,
		projectedContent: projectedContent,
		searchUses:       searchUses,
		searchEnabled:    searchEnabled,
		searchPolicy:     searchPolicy,
	}, true, nil
}

func (g *webSearchGateway) executeSearchCalls(ctx context.Context, calls []webSearchToolCall, policy webSearchPolicy, searchUses int) ([]json.RawMessage, []webSearchExecution, int, error) {
	results := make([]json.RawMessage, 0, len(calls))
	executions := make([]webSearchExecution, 0, len(calls))
	for _, call := range calls {
		if call.search == nil {
			return nil, nil, searchUses, fmt.Errorf("tool %q is not owned by the web search gateway", call.name)
		}
		if call.externalID == "" {
			externalID, err := serverWebSearchToolUseID(call.id)
			if err != nil {
				return nil, nil, searchUses, err
			}
			call.externalID = externalID
		}
		var searchErr error
		var errorCode string
		var result websearch.SearchResponse
		if policy.MaxUses > 0 && searchUses >= policy.MaxUses {
			searchErr = errors.New("web search max uses exceeded")
			errorCode = searchErrorMaxUses
		} else {
			searchUses++
			result, searchErr = g.search(ctx, call.search.Query, policy)
			if searchErr != nil {
				errorCode = searchErrorUnavailable
			}
		}
		if searchErr != nil {
			// 搜索失败会降级为 web_search_tool_result_error 继续本回合，调用方只看到
			// error_code；这里记录唯一一次可诊断的原因，不记录 query 等请求内容。
			g.logger.WarnContext(ctx, "web search execution degraded",
				"request_id", httpapi.RequestID(ctx),
				"error_code", errorCode,
				"error", searchErr,
			)
		}
		execution := webSearchExecution{call: call, results: result, err: searchErr, errorCode: errorCode}
		executions = append(executions, execution)
		toolResult, err := webSearchToolResult(execution)
		if err != nil {
			return nil, nil, searchUses, fmt.Errorf("encode web search tool result: %w", err)
		}
		results = append(results, toolResult)
	}
	return results, executions, searchUses, nil
}

func (g *webSearchGateway) prepareWebSearchTranscript(ctx context.Context, messages []json.RawMessage, policy webSearchPolicy) ([]json.RawMessage, []json.RawMessage, int, error) {
	pending, err := findPendingWebSearchTurn(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	paused, err := findPausedWebSearchTurn(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	transcript, err := projectWebSearchTranscript(messages)
	if err != nil {
		return nil, nil, 0, err
	}
	// max_uses is a per-request limit. BYOK continuations in this request share
	// the same count; pause_turn and mixed continuations are new requests and
	// therefore receive their full configured allowance.
	if pending == nil && paused == nil {
		return transcript, nil, 0, nil
	}
	if paused != nil {
		return g.resumePausedWebSearchTurn(ctx, transcript, paused, policy)
	}
	searchResults, executions, searchUses, err := g.executeSearchCalls(ctx, pending.searchCalls(), policy, 0)
	if err != nil {
		return nil, nil, 0, err
	}
	mergedResults, err := pending.mergeResults(searchResults)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(transcript) == 0 {
		return nil, nil, 0, errors.New("pending web search continuation has no transcript")
	}
	transcript[len(transcript)-1], err = replaceWebSearchMessageContent(transcript[len(transcript)-1], mergedResults)
	if err != nil {
		return nil, nil, 0, err
	}
	prefix, err := webSearchExecutionResultBlocks(executions)
	if err != nil {
		return nil, nil, 0, err
	}
	return transcript, prefix, searchUses, nil
}

func (g *webSearchGateway) resumePausedWebSearchTurn(ctx context.Context, transcript []json.RawMessage, pending *webSearchPendingTurn, policy webSearchPolicy) ([]json.RawMessage, []json.RawMessage, int, error) {
	searchResults, executions, searchUses, err := g.executeSearchCalls(ctx, pending.searchCalls(), policy, 0)
	if err != nil {
		return nil, nil, 0, err
	}
	resultMessage, err := webSearchUserMessage(searchResults)
	if err != nil {
		return nil, nil, 0, err
	}
	transcript = append(transcript, resultMessage)
	prefix, err := webSearchExecutionResultBlocks(executions)
	if err != nil {
		return nil, nil, 0, err
	}
	return transcript, prefix, searchUses, nil
}

func (g *webSearchGateway) send(ctx context.Context, body []byte, rawQuery string, headers http.Header) (webSearchGatewayResponse, error) {
	target, err := messagesEndpoint(g.upstreamBaseURL, rawQuery)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("build messages upstream endpoint: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("build messages upstream request: %w", err)
	}
	request.Header = sanitizedRequestHeaders(headers)
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("X-Api-Key", strings.TrimSpace(g.upstreamAPIKey))
	if request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	// 透传调用方的 Accept-Encoding 会关闭 Transport 的透明解压，使 gateway 拿到未解压的
	// gzip 字节并在解析上游 JSON 时失败；这里交回 Transport 自行协商并解压。
	request.Header.Del("Accept-Encoding")
	response, err := g.client.Do(request)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("send messages upstream request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxWebSearchResponseBytes+1))
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("read messages upstream response: %w", err)
	}
	if len(responseBody) > maxWebSearchResponseBytes {
		return webSearchGatewayResponse{}, errors.New("messages upstream response exceeds maximum size")
	}
	return webSearchGatewayResponse{statusCode: response.StatusCode, header: response.Header.Clone(), body: responseBody}, nil
}

// serverToolIterationLimit 返回每条入站请求允许的 BYOK 采样次数，未配置时回落到与
// Anthropic 对齐的默认值。返回值恒为正数。
func (g *webSearchGateway) serverToolIterationLimit() int {
	if g.maxServerToolIterations > 0 {
		return g.maxServerToolIterations
	}
	return defaultServerToolIterations
}

func (g *webSearchGateway) search(ctx context.Context, query string, policy webSearchPolicy) (websearch.SearchResponse, error) {
	return g.searcher.Search(ctx, websearch.SearchRequest{
		Query:   query,
		Options: webSearchOptions(policy),
	})
}

func webSearchOptions(policy webSearchPolicy) websearch.SearchOptions {
	return websearch.SearchOptions{
		MaxResults:     5,
		IncludeDomains: append([]string(nil), policy.AllowedDomains...),
		ExcludeDomains: append([]string(nil), policy.BlockedDomains...),
	}
}

func extractWebSearchToolCalls(body []byte) ([]webSearchToolCall, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Content == nil {
		return nil, errors.New("messages response content must be an array")
	}
	calls := []webSearchToolCall{}
	seenIDs := make(map[string]struct{})
	for _, rawBlock := range response.Content {
		var block webSearchContentBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode messages content block: %w", err)
		}
		if block.Type != "tool_use" {
			continue
		}
		if strings.TrimSpace(block.ID) == "" {
			return nil, errors.New("tool use id is required")
		}
		if _, duplicate := seenIDs[block.ID]; duplicate {
			return nil, fmt.Errorf("duplicate tool use id %q", block.ID)
		}
		seenIDs[block.ID] = struct{}{}
		call := webSearchToolCall{id: block.ID, name: block.Name, input: append(json.RawMessage(nil), block.Input...)}
		if block.Name != upstreamSearchToolName {
			calls = append(calls, call)
			continue
		}
		var input webSearchInput
		if len(block.Input) == 0 || json.Unmarshal(block.Input, &input) != nil {
			return nil, errors.New("web search tool input must be an object")
		}
		input.Query = strings.TrimSpace(input.Query)
		if input.Query == "" {
			return nil, errors.New("web search tool input query is required")
		}
		call.search = &input
		calls = append(calls, call)
	}
	return calls, nil
}
