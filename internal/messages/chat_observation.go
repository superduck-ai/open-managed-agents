package messages

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"time"
)

type chatObservation struct {
	finishReason string
}

type chatChunk struct {
	ID      string          `json:"id"`
	Choices []chatChoice    `json:"choices"`
	Usage   *chatUsage      `json:"usage"`
	Error   json.RawMessage `json:"error"`
}

type chatChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

type chatDelta struct {
	Content          string          `json:"content"`
	Refusal          string          `json:"refusal"`
	ReasoningContent string          `json:"reasoning_content"`
	ToolCalls        []chatToolDelta `json:"tool_calls"`
}

type chatToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	PromptDetails    *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (c *chatObservation) observe(o *responseObservation, data []byte) {
	if o.malformed || o.complete {
		return
	}
	if bytes.Equal(bytes.TrimSuffix(data, []byte{'\n'}), []byte("[DONE]")) {
		c.complete(o)
		return
	}
	var chunk chatChunk
	if json.Unmarshal(data, &chunk) != nil || len(chunk.Choices) > 1 {
		o.malformed = true
		return
	}
	if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
		o.result.ErrorType = "provider_error"
		o.malformed = true
		return
	}
	if chunk.ID != "" {
		if o.messageID != "" && o.messageID != chunk.ID {
			o.malformed = true
			return
		}
		o.messageID = chunk.ID
	}
	if chunk.Usage != nil {
		o.mergeChatUsage(chunk.Usage)
	}
	for _, choice := range chunk.Choices {
		if o.messageID == "" || choice.Index != 0 || c.finishReason != "" || choice.Delta.Refusal != "" || choice.Delta.ReasoningContent != "" {
			o.malformed = true
			return
		}
		o.observeChatDelta(choice.Delta)
		if choice.FinishReason != "" {
			c.finishReason = choice.FinishReason
		}
	}
}

func (o *responseObservation) mergeChatUsage(usage *chatUsage) {
	if usage.PromptTokens == nil || usage.CompletionTokens == nil || *usage.PromptTokens < 0 || *usage.CompletionTokens < 0 {
		o.malformed = true
		return
	}
	o.result.Usage.InputTokens = usage.PromptTokens
	o.result.Usage.OutputTokens = usage.CompletionTokens
	if usage.PromptDetails == nil || usage.PromptDetails.CachedTokens == nil {
		return
	}
	cached := *usage.PromptDetails.CachedTokens
	if cached < 0 || (usage.PromptTokens != nil && cached > *usage.PromptTokens) {
		o.malformed = true
		return
	}
	o.result.Usage.CacheReadInputTokens = usage.PromptDetails.CachedTokens
	if usage.PromptTokens != nil {
		uncached := *usage.PromptTokens - cached
		o.result.Usage.InputTokens = &uncached
	}
}

func (o *responseObservation) observeChatDelta(delta chatDelta) {
	if delta.Content != "" {
		block := o.chatBlock(0, "text")
		block.textBuffer.WriteString(delta.Content)
		o.contentBytes += len(delta.Content)
	}
	for _, tool := range delta.ToolCalls {
		if tool.Index < 0 || tool.Index >= 1024 || (tool.Type != "" && tool.Type != "function") {
			o.malformed = true
			return
		}
		block := o.chatBlock(tool.Index+1, "tool_use")
		block.ID += tool.ID
		block.Name += tool.Function.Name
		block.inputBuffer.WriteString(tool.Function.Arguments)
		o.contentBytes += len(tool.ID) + len(tool.Function.Name) + len(tool.Function.Arguments)
	}
	if o.contentBytes > 4*1024*1024 {
		o.malformed = true
		o.result.ErrorType = "observation_limit"
	}
}

func (o *responseObservation) chatBlock(index int, kind string) *responseContentBlock {
	if o.blocks == nil {
		o.blocks = make(map[int]*responseContentBlock)
	}
	if o.blocks[index] == nil {
		o.blocks[index] = &responseContentBlock{Type: kind}
	}
	return o.blocks[index]
}

func (c *chatObservation) complete(o *responseObservation) {
	if o.messageID == "" || len(o.blocks) == 0 || !validChatFinish(c.finishReason, o.blocks) {
		o.malformed = true
		return
	}
	for _, index := range slices.Sorted(maps.Keys(o.blocks)) {
		block := o.blocks[index]
		block.Text = block.textBuffer.String()
		if block.Type == "tool_use" && block.inputBuffer.Len() == 0 {
			block.Input = json.RawMessage(`{}`)
		}
		o.addEventID(index, block.Type)
		o.addMessage(index, *block)
		if block.Type == "tool_use" {
			o.result.ToolUseIDs = append(o.result.ToolUseIDs, block.ID)
		}
	}
	o.complete = true
	o.result.EndedAt = time.Now().UTC().Truncate(time.Microsecond)
	if o.onComplete != nil {
		o.onComplete()
	}
}

func validChatFinish(reason string, blocks map[int]*responseContentBlock) bool {
	hasTools := false
	for _, block := range blocks {
		hasTools = hasTools || block.Type == "tool_use"
	}
	switch reason {
	case "tool_calls":
		return hasTools
	case "stop", "length":
		return !hasTools
	default:
		return false
	}
}
