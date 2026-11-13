package messages

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

type responsesObservation struct{}

func (*responsesObservation) observe(o *responseObservation, data []byte) {
	if o.complete || o.malformed {
		return
	}
	response, raw, terminal, err := billing.ParseResponse(data, o.streaming)
	if err != nil {
		o.malformed = true
		return
	}
	if !terminal {
		return
	}
	if o.request != nil {
		o.result.ProviderCost = o.request.ResponseCost.Extract(raw)
	}
	usage, valid := response.Usage.BillingUsage()
	if !valid {
		o.malformed = true
		return
	}
	o.result.Usage = codesessions.ModelRequestUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadInputTokens: usage.CacheReadTokens}
	o.complete = true
	o.result.EndedAt = time.Now().UTC().Truncate(time.Microsecond)
	o.stopReason = "end_turn"
	switch response.Status {
	case "failed":
		o.result.ErrorType = "provider_error"
	case "incomplete":
		o.result.ErrorType = "incomplete_response"
		o.stopReason = "max_tokens"
	case "completed":
		o.responseMessages(response)
	}
	if o.streaming && o.onComplete != nil {
		o.onComplete()
	}
}

func (o *responseObservation) responseMessages(response billing.ResponsePayload) {
	o.messageID = response.ID
	index := 0
	for _, item := range response.Output {
		if item.Type == "reasoning" {
			for range max(1, len(item.Summary)) {
				block := responseContentBlock{Type: "thinking"}
				o.addEventID(index, block.Type)
				o.addMessage(index, block)
				index++
			}
			continue
		}
		if item.Type == "function_call" {
			block := responseContentBlock{ID: item.CallID, Type: "tool_use", Name: item.Name, Input: json.RawMessage(item.Arguments)}
			o.addMessage(index, block)
			o.result.ToolUseIDs = append(o.result.ToolUseIDs, item.CallID)
			index++
			o.stopReason = "tool_use"
			continue
		}
		if item.Type != "message" {
			continue
		}
		var text strings.Builder
		for _, content := range item.Content {
			if content.Type == "output_text" {
				text.WriteString(content.Text)
			}
		}
		block := responseContentBlock{Type: "text", Text: text.String()}
		o.addEventID(index, block.Type)
		o.addMessage(index, block)
		index++
	}
}
