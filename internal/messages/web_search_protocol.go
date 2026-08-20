package messages

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// serverToolUseIDPrefix 是 Anthropic server tool 的 ID 前缀。所有上游 tool ID
	// 都按 opaque 字符串编码，避免依赖 provider 的 ID 格式。
	serverToolUseIDPrefix          = "srvtoolu_"
	encodedUpstreamToolUseIDPrefix = "oma_encoded_"
)

// The declarations below project persisted Anthropic message history between
// public server-tool blocks and the ordinary BYOK tool transcript.
type webSearchMessageEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type webSearchProtocolBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type webSearchProjectedMessage struct {
	role   string
	blocks []json.RawMessage
}

type webSearchPendingTurn struct {
	orderedCalls  []webSearchToolCall
	clientResults map[string]json.RawMessage
}

func hasWebSearchHistory(messages []json.RawMessage) bool {
	for _, rawMessage := range messages {
		message, err := decodeWebSearchMessage(rawMessage)
		if err != nil || message.Role != "assistant" {
			continue
		}
		var blocks []json.RawMessage
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		for _, rawBlock := range blocks {
			var block webSearchProtocolBlock
			if json.Unmarshal(rawBlock, &block) != nil {
				continue
			}
			if block.Type == "web_search_tool_result" ||
				(block.Type == "server_tool_use" && block.Name == searchToolName) {
				return true
			}
		}
	}
	return false
}

func webSearchCalls(calls []webSearchToolCall) []webSearchToolCall {
	searchCalls := make([]webSearchToolCall, 0, len(calls))
	for _, call := range calls {
		if call.search != nil {
			searchCalls = append(searchCalls, call)
		}
	}
	return searchCalls
}

func isWebSearchPauseContinuation(messages []json.RawMessage) bool {
	if len(messages) == 0 {
		return false
	}
	message, err := decodeWebSearchMessage(messages[len(messages)-1])
	if err != nil || message.Role != "assistant" {
		return false
	}
	var blocks []json.RawMessage
	if json.Unmarshal(message.Content, &blocks) != nil {
		return false
	}
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) == nil &&
			(block.Type == "web_search_tool_result" ||
				(block.Type == "server_tool_use" && block.Name == searchToolName)) {
			return true
		}
	}
	return false
}

func findPausedWebSearchTurn(messages []json.RawMessage) (*webSearchPendingTurn, error) {
	if !isWebSearchPauseContinuation(messages) {
		return nil, nil
	}
	assistant, err := decodeWebSearchMessage(messages[len(messages)-1])
	if err != nil {
		return nil, err
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(assistant.Content, &blocks); err != nil {
		return nil, err
	}
	completedSearches := make(map[string]struct{})
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) == nil && block.Type == "web_search_tool_result" {
			completedSearches[block.ToolUseID] = struct{}{}
		}
	}
	turn := &webSearchPendingTurn{clientResults: make(map[string]json.RawMessage)}
	serverToolUses := make(map[string]struct{})
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode paused assistant content block: %w", err)
		}
		if block.Type == "tool_use" {
			return nil, errors.New("pause_turn continuation cannot contain a pending client tool use")
		}
		if block.Type != "server_tool_use" || block.Name != searchToolName {
			continue
		}
		if _, duplicate := serverToolUses[block.ID]; duplicate {
			return nil, fmt.Errorf("duplicate server web search tool use id %q", block.ID)
		}
		serverToolUses[block.ID] = struct{}{}
		if _, complete := completedSearches[block.ID]; complete {
			continue
		}
		call, err := pendingWebSearchCall(block)
		if err != nil {
			return nil, err
		}
		turn.orderedCalls = append(turn.orderedCalls, call)
	}
	if len(turn.orderedCalls) == 0 {
		return nil, nil
	}
	return turn, nil
}

func findPendingWebSearchTurn(messages []json.RawMessage) (*webSearchPendingTurn, error) {
	if len(messages) < 2 {
		return nil, nil
	}
	assistant, err := decodeWebSearchMessage(messages[len(messages)-2])
	if err != nil {
		return nil, err
	}
	user, err := decodeWebSearchMessage(messages[len(messages)-1])
	if err != nil {
		return nil, err
	}
	if assistant.Role != "assistant" || user.Role != "user" {
		return nil, nil
	}
	var assistantBlocks []json.RawMessage
	if err := json.Unmarshal(assistant.Content, &assistantBlocks); err != nil {
		return nil, nil
	}
	completedSearches := make(map[string]struct{})
	for _, rawBlock := range assistantBlocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) == nil && block.Type == "web_search_tool_result" {
			completedSearches[block.ToolUseID] = struct{}{}
		}
	}
	turn := &webSearchPendingTurn{clientResults: make(map[string]json.RawMessage)}
	clientCalls := make(map[string]struct{})
	serverToolUses := make(map[string]struct{})
	pendingSearches := 0
	for _, rawBlock := range assistantBlocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode assistant content block: %w", err)
		}
		switch {
		case block.Type == "server_tool_use" && block.Name == searchToolName:
			if _, duplicate := serverToolUses[block.ID]; duplicate {
				return nil, fmt.Errorf("duplicate server web search tool use id %q", block.ID)
			}
			serverToolUses[block.ID] = struct{}{}
			if _, complete := completedSearches[block.ID]; complete {
				continue
			}
			call, err := pendingWebSearchCall(block)
			if err != nil {
				return nil, err
			}
			turn.orderedCalls = append(turn.orderedCalls, call)
			pendingSearches++
		case block.Type == "tool_use":
			if strings.TrimSpace(block.ID) == "" {
				return nil, errors.New("client tool use id is required")
			}
			turn.orderedCalls = append(turn.orderedCalls, webSearchToolCall{id: block.ID, name: block.Name, input: block.Input})
			clientCalls[block.ID] = struct{}{}
		}
	}
	if pendingSearches == 0 {
		return nil, nil
	}
	if len(clientCalls) == 0 {
		return nil, errors.New("pending web search continuation is missing a client tool use")
	}
	var userBlocks []json.RawMessage
	if err := json.Unmarshal(user.Content, &userBlocks); err != nil {
		return nil, errors.New("mixed tool continuation must contain tool_result blocks")
	}
	for _, rawBlock := range userBlocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode client tool result: %w", err)
		}
		if block.Type != "tool_result" {
			return nil, errors.New("mixed tool continuation must contain only tool_result blocks")
		}
		if _, ok := clientCalls[block.ToolUseID]; !ok {
			return nil, fmt.Errorf("unexpected client tool result %q", block.ToolUseID)
		}
		if _, duplicate := turn.clientResults[block.ToolUseID]; duplicate {
			return nil, fmt.Errorf("duplicate client tool result %q", block.ToolUseID)
		}
		turn.clientResults[block.ToolUseID] = append(json.RawMessage(nil), rawBlock...)
	}
	for id := range clientCalls {
		if _, ok := turn.clientResults[id]; !ok {
			return nil, fmt.Errorf("client tool result %q is required", id)
		}
	}
	return turn, nil
}

func pendingWebSearchCall(block webSearchProtocolBlock) (webSearchToolCall, error) {
	if strings.TrimSpace(block.ID) == "" {
		return webSearchToolCall{}, errors.New("server web search tool use id is required")
	}
	upstreamID, err := upstreamWebSearchToolUseID(block.ID)
	if err != nil {
		return webSearchToolCall{}, err
	}
	var input webSearchInput
	if len(block.Input) == 0 || json.Unmarshal(block.Input, &input) != nil {
		return webSearchToolCall{}, errors.New("server web search tool input must be an object")
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return webSearchToolCall{}, errors.New("server web search query is required")
	}
	return webSearchToolCall{
		id:         upstreamID,
		externalID: block.ID,
		name:       upstreamSearchToolName,
		input:      append(json.RawMessage(nil), block.Input...),
		search:     &input,
	}, nil
}

func (t *webSearchPendingTurn) searchCalls() []webSearchToolCall {
	calls := make([]webSearchToolCall, 0, len(t.orderedCalls))
	for _, call := range t.orderedCalls {
		if call.search != nil {
			calls = append(calls, call)
		}
	}
	return calls
}

func (t *webSearchPendingTurn) mergeResults(searchResults []json.RawMessage) ([]json.RawMessage, error) {
	byID := make(map[string]json.RawMessage, len(searchResults))
	for _, rawResult := range searchResults {
		var result webSearchProtocolBlock
		if err := json.Unmarshal(rawResult, &result); err != nil {
			return nil, fmt.Errorf("decode web search tool result: %w", err)
		}
		byID[result.ToolUseID] = rawResult
	}
	merged := make([]json.RawMessage, 0, len(t.orderedCalls))
	for _, call := range t.orderedCalls {
		if call.search != nil {
			result, ok := byID[call.id]
			if !ok {
				return nil, fmt.Errorf("web search tool result %q is missing", call.id)
			}
			merged = append(merged, result)
			continue
		}
		result, ok := t.clientResults[call.id]
		if !ok {
			return nil, fmt.Errorf("client tool result %q is missing", call.id)
		}
		merged = append(merged, result)
	}
	return merged, nil
}

func projectWebSearchTranscript(messages []json.RawMessage) ([]json.RawMessage, error) {
	projected := make([]json.RawMessage, 0, len(messages))
	for _, rawMessage := range messages {
		message, err := decodeWebSearchMessage(rawMessage)
		if err != nil {
			return nil, err
		}
		if message.Role != "assistant" {
			projected = append(projected, append(json.RawMessage(nil), rawMessage...))
			continue
		}
		var blocks []json.RawMessage
		if err := json.Unmarshal(message.Content, &blocks); err != nil {
			projected = append(projected, append(json.RawMessage(nil), rawMessage...))
			continue
		}
		segments, err := projectAssistantWebSearchMessages(blocks)
		if err != nil {
			return nil, err
		}
		for _, segment := range segments {
			projected, err = appendWebSearchProjectedMessage(projected, segment)
			if err != nil {
				return nil, err
			}
		}
	}
	return projected, nil
}

func projectAssistantWebSearchMessages(blocks []json.RawMessage) ([]webSearchProjectedMessage, error) {
	segments := make([]webSearchProjectedMessage, 0, 3)
	assistantBlocks := make([]json.RawMessage, 0, len(blocks))
	userBlocks := make([]json.RawMessage, 0, len(blocks))
	flush := func(role string, pending *[]json.RawMessage) {
		if len(*pending) == 0 {
			return
		}
		segments = append(segments, webSearchProjectedMessage{role: role, blocks: *pending})
		*pending = nil
	}
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode assistant content block: %w", err)
		}
		if block.Type == "web_search_tool_result" {
			flush("assistant", &assistantBlocks)
			result, err := projectServerResultToClient(block)
			if err != nil {
				return nil, err
			}
			userBlocks = append(userBlocks, result)
			continue
		}
		flush("user", &userBlocks)
		if block.Type == "server_tool_use" && block.Name == searchToolName {
			projectedBlock, err := projectServerToolUseToClient(rawBlock, block)
			if err != nil {
				return nil, err
			}
			assistantBlocks = append(assistantBlocks, projectedBlock)
			continue
		}
		assistantBlocks = append(assistantBlocks, append(json.RawMessage(nil), rawBlock...))
	}
	flush("user", &userBlocks)
	flush("assistant", &assistantBlocks)
	return segments, nil
}

func appendWebSearchProjectedMessage(transcript []json.RawMessage, message webSearchProjectedMessage) ([]json.RawMessage, error) {
	if message.role == "user" && webSearchToolResultsOnly(message.blocks) && len(transcript) > 0 {
		last, err := decodeWebSearchMessage(transcript[len(transcript)-1])
		if err != nil {
			return nil, err
		}
		var lastBlocks []json.RawMessage
		if last.Role == "user" && json.Unmarshal(last.Content, &lastBlocks) == nil && webSearchToolResultsOnly(lastBlocks) {
			merged := append(lastBlocks, message.blocks...)
			if len(transcript) >= 2 {
				merged = orderWebSearchToolResults(transcript[len(transcript)-2], merged)
			}
			transcript[len(transcript)-1], err = replaceWebSearchMessageContent(transcript[len(transcript)-1], merged)
			return transcript, err
		}
	}
	encoded, err := marshalWebSearchMessage(message.role, message.blocks)
	if err != nil {
		return nil, err
	}
	return append(transcript, encoded), nil
}

func webSearchToolResultsOnly(blocks []json.RawMessage) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "tool_result" {
			return false
		}
	}
	return true
}

func orderWebSearchToolResults(rawAssistant json.RawMessage, results []json.RawMessage) []json.RawMessage {
	assistant, err := decodeWebSearchMessage(rawAssistant)
	if err != nil || assistant.Role != "assistant" {
		return results
	}
	var blocks []json.RawMessage
	if json.Unmarshal(assistant.Content, &blocks) != nil {
		return results
	}
	// Anthropic 按 tool_use_id 匹配 result，不依赖 result 在 user message 里的顺序，
	// 因此这里只做与 tool_use 一致的可读性排序。按 ID 排队而不是按 ID 建立唯一索引，
	// 是为了让重复 ID 的 result 也全部保留：丢弃 result 会让对应的 tool_use 失去配对。
	pendingByID := make(map[string][]int, len(results))
	for index, rawResult := range results {
		var result webSearchProtocolBlock
		if json.Unmarshal(rawResult, &result) == nil {
			pendingByID[result.ToolUseID] = append(pendingByID[result.ToolUseID], index)
		}
	}
	ordered := make([]json.RawMessage, 0, len(results))
	emitted := make([]bool, len(results))
	for _, rawBlock := range blocks {
		var block webSearchProtocolBlock
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "tool_use" {
			continue
		}
		queue := pendingByID[block.ID]
		if len(queue) == 0 {
			continue
		}
		pendingByID[block.ID] = queue[1:]
		ordered = append(ordered, results[queue[0]])
		emitted[queue[0]] = true
	}
	for index, rawResult := range results {
		if !emitted[index] {
			ordered = append(ordered, rawResult)
		}
	}
	return ordered
}

// projectServerToolUseToClient 把历史里的 server_tool_use 还原为 BYOK 的 ordinary
// tool_use，同时把协议名换成 gateway 实际声明给 BYOK 的独占工具名。
func projectServerToolUseToClient(rawBlock json.RawMessage, block webSearchProtocolBlock) (json.RawMessage, error) {
	upstreamID, err := upstreamWebSearchToolUseID(block.ID)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBlock, &fields); err != nil {
		return nil, err
	}
	fields["type"] = json.RawMessage(`"tool_use"`)
	encodedID, err := json.Marshal(upstreamID)
	if err != nil {
		return nil, err
	}
	fields["id"] = encodedID
	encodedName, err := json.Marshal(upstreamSearchToolName)
	if err != nil {
		return nil, err
	}
	fields["name"] = encodedName
	return json.Marshal(fields)
}

func projectServerResultToClient(block webSearchProtocolBlock) (json.RawMessage, error) {
	upstreamID, err := upstreamWebSearchToolUseID(block.ToolUseID)
	if err != nil {
		return nil, err
	}
	if errorCode := webSearchResultErrorCode(block.Content); errorCode != "" {
		message := `"web search unavailable"`
		if errorCode == searchErrorMaxUses {
			message = `"web search max uses exceeded"`
		}
		return marshalWebSearchToolResult(webSearchToolResultBlock{
			Type: "tool_result", ToolUseID: upstreamID, IsError: true, Content: json.RawMessage(message),
		})
	}
	var searchResults []webSearchResultBlock
	if err := json.Unmarshal(block.Content, &searchResults); err != nil {
		return nil, fmt.Errorf("decode web search result content: %w", err)
	}
	for index := range searchResults {
		searchResults[index].Type = ""
		searchResults[index].EncryptedContent = ""
	}
	content, err := json.Marshal(searchResults)
	if err != nil {
		return nil, err
	}
	return marshalWebSearchToolResult(webSearchToolResultBlock{Type: "tool_result", ToolUseID: upstreamID, Content: content})
}

func decodeWebSearchMessage(rawMessage json.RawMessage) (webSearchMessageEnvelope, error) {
	var message webSearchMessageEnvelope
	if err := json.Unmarshal(rawMessage, &message); err != nil {
		return webSearchMessageEnvelope{}, fmt.Errorf("decode messages transcript entry: %w", err)
	}
	return message, nil
}

func marshalWebSearchMessage(role string, blocks []json.RawMessage) (json.RawMessage, error) {
	encoded, err := json.Marshal(struct {
		Role    string            `json:"role"`
		Content []json.RawMessage `json:"content"`
	}{Role: role, Content: blocks})
	if err != nil {
		return nil, fmt.Errorf("encode %s messages transcript entry: %w", role, err)
	}
	return encoded, nil
}

func replaceWebSearchMessageContent(rawMessage json.RawMessage, blocks []json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawMessage, &fields); err != nil {
		return nil, fmt.Errorf("decode messages transcript entry: %w", err)
	}
	content, err := json.Marshal(blocks)
	if err != nil {
		return nil, fmt.Errorf("encode messages transcript content: %w", err)
	}
	fields["content"] = content
	return json.Marshal(fields)
}

func projectPendingWebSearchContent(content []json.RawMessage) ([]json.RawMessage, error) {
	projected := make([]json.RawMessage, 0, len(content))
	for _, rawBlock := range content {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode mixed tool content block: %w", err)
		}
		if block.Type == "tool_use" && block.Name == upstreamSearchToolName {
			externalID, err := serverWebSearchToolUseID(block.ID)
			if err != nil {
				return nil, err
			}
			serverBlock, err := projectClientSearchCallToServer(rawBlock, externalID)
			if err != nil {
				return nil, err
			}
			projected = append(projected, serverBlock)
			continue
		}
		projected = append(projected, append(json.RawMessage(nil), rawBlock...))
	}
	return projected, nil
}

func projectCompletedWebSearchContent(content []json.RawMessage, executions []webSearchExecution) ([]json.RawMessage, error) {
	byID := make(map[string]webSearchExecution, len(executions))
	for _, execution := range executions {
		byID[execution.call.id] = execution
	}
	projected := make([]json.RawMessage, 0, len(content)+len(executions))
	for _, rawBlock := range content {
		var block webSearchProtocolBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode web search content block: %w", err)
		}
		if block.Type != "tool_use" || block.Name != upstreamSearchToolName {
			projected = append(projected, append(json.RawMessage(nil), rawBlock...))
			continue
		}
		execution, ok := byID[block.ID]
		if !ok {
			return nil, fmt.Errorf("web search execution %q is missing", block.ID)
		}
		externalID, err := execution.serverToolUseID()
		if err != nil {
			return nil, err
		}
		serverBlock, err := projectClientSearchCallToServer(rawBlock, externalID)
		if err != nil {
			return nil, err
		}
		resultBlock, err := webSearchServerResultBlock(execution)
		if err != nil {
			return nil, err
		}
		projected = append(projected, serverBlock, resultBlock)
	}
	return projected, nil
}

// projectClientSearchCallToServer 把 BYOK 的 ordinary tool_use 投影为面向调用方的
// server_tool_use，并把独占工具名换回 Anthropic 协议名，避免内部名字泄漏给调用方。
func projectClientSearchCallToServer(rawBlock json.RawMessage, externalID string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBlock, &fields); err != nil {
		return nil, err
	}
	fields["type"] = json.RawMessage(`"server_tool_use"`)
	encodedID, err := json.Marshal(externalID)
	if err != nil {
		return nil, err
	}
	fields["id"] = encodedID
	encodedName, err := json.Marshal(searchToolName)
	if err != nil {
		return nil, err
	}
	fields["name"] = encodedName
	return json.Marshal(fields)
}

func webSearchExecutionResultBlocks(executions []webSearchExecution) ([]json.RawMessage, error) {
	blocks := make([]json.RawMessage, 0, len(executions))
	for _, execution := range executions {
		block, err := webSearchServerResultBlock(execution)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func webSearchServerResultBlock(execution webSearchExecution) (json.RawMessage, error) {
	externalID, err := execution.serverToolUseID()
	if err != nil {
		return nil, err
	}
	if execution.err != nil {
		errorCode := execution.errorCode
		if errorCode == "" {
			errorCode = searchErrorUnavailable
		}
		return json.Marshal(struct {
			Type      string `json:"type"`
			ToolUseID string `json:"tool_use_id"`
			Content   struct {
				Type      string `json:"type"`
				ErrorCode string `json:"error_code"`
			} `json:"content"`
		}{Type: "web_search_tool_result", ToolUseID: externalID, Content: struct {
			Type      string `json:"type"`
			ErrorCode string `json:"error_code"`
		}{Type: "web_search_tool_result_error", ErrorCode: errorCode}})
	}
	resultContent := make([]webSearchResultBlock, 0, len(execution.results.Results))
	for _, result := range execution.results.Results {
		resultContent = append(resultContent, resultToServerContentItem(result))
	}
	encodedContent, err := json.Marshal(resultContent)
	if err != nil {
		return nil, fmt.Errorf("marshal web search content: %w", err)
	}
	return json.Marshal(struct {
		Type      string          `json:"type"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}{Type: "web_search_tool_result", ToolUseID: externalID, Content: encodedContent})
}

// serverWebSearchToolUseID 把 BYOK 的 ordinary tool_use ID 映射为面向调用方的 server
// tool ID。所有 provider-owned ID 都用带版本标记的 URL-safe 编码保存，保证映射唯一
// 且回放时能无损恢复原始 ID。
func serverWebSearchToolUseID(upstreamID string) (string, error) {
	if strings.TrimSpace(upstreamID) == "" {
		return "", errors.New("upstream tool use id is required")
	}
	return serverToolUseIDPrefix + encodedUpstreamToolUseIDPrefix + base64.RawURLEncoding.EncodeToString([]byte(upstreamID)), nil
}

// upstreamWebSearchToolUseID 是 serverWebSearchToolUseID 的逆映射，只接受当前 gateway
// 铸造的带版本标记的 opaque-ID 编码。
func upstreamWebSearchToolUseID(externalID string) (string, error) {
	suffix, ok := strings.CutPrefix(externalID, serverToolUseIDPrefix)
	if !ok || suffix == "" {
		return "", fmt.Errorf("invalid server tool use id %q", externalID)
	}
	if encoded, ok := strings.CutPrefix(suffix, encodedUpstreamToolUseIDPrefix); ok {
		upstreamID, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || strings.TrimSpace(string(upstreamID)) == "" {
			return "", fmt.Errorf("invalid encoded server tool use id %q", externalID)
		}
		return string(upstreamID), nil
	}
	return "", fmt.Errorf("invalid encoded server tool use id %q", externalID)
}
