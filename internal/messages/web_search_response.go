package messages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/websearch"
)

// webSearchGatewayResponse is the opaque HTTP response carried through the
// response projection and final encoding steps.
type webSearchGatewayResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

type webSearchContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	Text  string          `json:"text,omitempty"`
}

type webSearchToolResultBlock struct {
	Type      string          `json:"type"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content"`
}

type webSearchResultBlock struct {
	Type    string `json:"type,omitempty"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content,omitempty"`
	// EncryptedContent is opaque provider data. OMA-managed search leaves it empty.
	EncryptedContent string `json:"encrypted_content,omitempty"`
	PublishedDate    string `json:"published_date,omitempty"`
	PageAge          string `json:"page_age,omitempty"`
}

func webSearchResponseContent(body []byte) ([]json.RawMessage, error) {
	var response struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Content == nil {
		return nil, errors.New("messages response content must be an array")
	}
	return response.Content, nil
}

// webSearchUsageAccumulator combines the token accounting from all BYOK
// samples made for one inbound request.
type webSearchUsageAccumulator struct {
	totals  map[string]int64
	last    map[string]json.RawMessage
	samples int
}

func (u *webSearchUsageAccumulator) add(body []byte) error {
	var response struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode messages usage: %w", err)
	}
	if len(response.Usage) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Usage, &fields); err != nil {
		return fmt.Errorf("decode messages usage: %w", err)
	}
	if len(fields) == 0 {
		return nil
	}
	if u.totals == nil {
		u.totals = make(map[string]int64, len(fields))
	}
	for name, value := range fields {
		if count, ok := webSearchUsageCount(value); ok {
			u.totals[name] += count
		}
	}
	u.last = fields
	u.samples++
	return nil
}

func (u *webSearchUsageAccumulator) merge() (map[string]json.RawMessage, bool) {
	if u == nil || u.samples == 0 {
		return nil, false
	}
	merged := cloneRawMap(u.last)
	if u.samples < 2 {
		return merged, false
	}
	for name, total := range u.totals {
		encoded, err := json.Marshal(total)
		if err != nil {
			continue
		}
		merged[name] = encoded
	}
	return merged, true
}

func (u *webSearchUsageAccumulator) webSearchUsage(searchRequests int) (json.RawMessage, bool, error) {
	merged, changed := u.merge()
	if searchRequests > 0 {
		if merged == nil {
			merged = make(map[string]json.RawMessage, 1)
		}
		serverToolUse, err := webSearchServerToolUsage(merged["server_tool_use"], searchRequests)
		if err != nil {
			return nil, false, err
		}
		merged["server_tool_use"] = serverToolUse
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return nil, false, fmt.Errorf("encode messages usage: %w", err)
	}
	return encoded, true, nil
}

func webSearchServerToolUsage(existing json.RawMessage, searchRequests int) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &fields); err != nil {
			return nil, fmt.Errorf("decode messages server tool usage: %w", err)
		}
	}
	encodedRequests, err := json.Marshal(searchRequests)
	if err != nil {
		return nil, fmt.Errorf("encode web search request count: %w", err)
	}
	fields["web_search_requests"] = encodedRequests
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode messages server tool usage: %w", err)
	}
	return encoded, nil
}

func countBillableWebSearchRequests(content []json.RawMessage) int {
	requests := 0
	for _, rawBlock := range content {
		var block struct {
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(rawBlock, &block) != nil || block.Type != "web_search_tool_result" {
			continue
		}
		if webSearchResultErrorCode(block.Content) != "" {
			continue
		}
		requests++
	}
	return requests
}

func webSearchResultErrorCode(content json.RawMessage) string {
	var resultError struct {
		Type      string `json:"type"`
		ErrorCode string `json:"error_code"`
	}
	if json.Unmarshal(content, &resultError) != nil || resultError.Type != "web_search_tool_result_error" {
		return ""
	}
	return resultError.ErrorCode
}

func webSearchUsageCount(value json.RawMessage) (int64, bool) {
	var number json.Number
	if err := json.Unmarshal(value, &number); err != nil {
		return 0, false
	}
	count, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return count, true
}

func finalizeWebSearchResponse(response webSearchGatewayResponse, content []json.RawMessage, stream bool, stopReason string, usage *webSearchUsageAccumulator) (webSearchGatewayResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &fields); err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("decode messages response: %w", err)
	}
	encodedContent, err := json.Marshal(content)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("encode messages response content: %w", err)
	}
	fields["content"] = encodedContent
	if stopReason != "" {
		encodedStopReason, err := json.Marshal(stopReason)
		if err != nil {
			return webSearchGatewayResponse{}, fmt.Errorf("encode messages stop reason: %w", err)
		}
		fields["stop_reason"] = encodedStopReason
	}
	mergedUsage, changed, err := usage.webSearchUsage(countBillableWebSearchRequests(content))
	if err != nil {
		return webSearchGatewayResponse{}, err
	}
	if changed {
		fields["usage"] = mergedUsage
	}
	response.body, err = json.Marshal(fields)
	if err != nil {
		return webSearchGatewayResponse{}, fmt.Errorf("encode messages response: %w", err)
	}
	if stream {
		response.body, err = encodeWebSearchSSE(response.body)
		if err != nil {
			return webSearchGatewayResponse{}, fmt.Errorf("encode messages stream: %w", err)
		}
		response.header.Set("Content-Type", "text/event-stream")
		prepareResponseHeaders(response.header)
	}
	response.header.Del("Content-Length")
	return response, nil
}

func webSearchAssistantMessage(body []byte) (json.RawMessage, error) {
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

func webSearchUserMessage(results []json.RawMessage) (json.RawMessage, error) {
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

func webSearchToolResult(execution webSearchExecution) (json.RawMessage, error) {
	if execution.err != nil {
		message := `"web search unavailable"`
		if execution.errorCode == searchErrorMaxUses {
			message = `"web search max uses exceeded"`
		}
		return marshalWebSearchToolResult(webSearchToolResultBlock{
			Type: "tool_result", ToolUseID: execution.call.id, IsError: true,
			Content: json.RawMessage(message),
		})
	}
	content := make([]webSearchResultBlock, 0, len(execution.results.Results))
	for _, result := range execution.results.Results {
		content = append(content, resultToUpstreamContentItem(result))
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshal web search results: %w", err)
	}
	return marshalWebSearchToolResult(webSearchToolResultBlock{Type: "tool_result", ToolUseID: execution.call.id, Content: encoded})
}

func marshalWebSearchToolResult(result webSearchToolResultBlock) (json.RawMessage, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal tool result: %w", err)
	}
	return encoded, nil
}

func resultToUpstreamContentItem(result websearch.Result) webSearchResultBlock {
	content := result.Snippet
	if result.Text != "" {
		content = result.Text
	}
	return webSearchResultBlock{
		Title:         result.Title,
		URL:           result.URL,
		Content:       content,
		PublishedDate: result.PublishedDate,
		PageAge:       result.PageAge,
	}
}

func resultToServerContentItem(result websearch.Result) webSearchResultBlock {
	return webSearchResultBlock{
		Type:    "web_search_result",
		Title:   result.Title,
		URL:     result.URL,
		PageAge: result.PageAge,
	}
}

func encodeWebSearchSSE(body []byte) ([]byte, error) {
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
	if err := writeWebSearchSSE(&output, "message_start", struct {
		Type    string                     `json:"type"`
		Message map[string]json.RawMessage `json:"message"`
	}{Type: "message_start", Message: messageStart}); err != nil {
		return nil, err
	}
	for index, rawBlock := range content {
		var block webSearchContentBlock
		if err := json.Unmarshal(rawBlock, &block); err != nil {
			return nil, fmt.Errorf("decode messages content block: %w", err)
		}
		startBlock := append(json.RawMessage(nil), rawBlock...)
		var toolInput json.RawMessage
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
		} else if block.Type == "tool_use" || block.Type == "server_tool_use" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(rawBlock, &fields); err != nil {
				return nil, fmt.Errorf("decode tool content block: %w", err)
			}
			toolInput = append(json.RawMessage(nil), fields["input"]...)
			fields["input"] = json.RawMessage(`{}`)
			var err error
			startBlock, err = json.Marshal(fields)
			if err != nil {
				return nil, fmt.Errorf("marshal tool content block: %w", err)
			}
		}
		if err := writeWebSearchSSE(&output, "content_block_start", struct {
			Type         string          `json:"type"`
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
		}{Type: "content_block_start", Index: index, ContentBlock: startBlock}); err != nil {
			return nil, err
		}
		if block.Text != "" {
			if err := writeWebSearchSSE(&output, "content_block_delta", struct {
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
		} else if len(toolInput) > 0 {
			if err := writeWebSearchSSE(&output, "content_block_delta", struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}{Type: "content_block_delta", Index: index, Delta: struct {
				Type        string `json:"type"`
				PartialJSON string `json:"partial_json"`
			}{Type: "input_json_delta", PartialJSON: string(toolInput)}}); err != nil {
				return nil, err
			}
		}
		if err := writeWebSearchSSE(&output, "content_block_stop", struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
		}{Type: "content_block_stop", Index: index}); err != nil {
			return nil, err
		}
	}
	if err := writeWebSearchSSE(&output, "message_delta", struct {
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
	if err := writeWebSearchSSE(&output, "message_stop", struct {
		Type string `json:"type"`
	}{Type: "message_stop"}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeWebSearchSSE(output *bytes.Buffer, event string, value any) error {
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
