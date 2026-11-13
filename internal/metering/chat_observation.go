package metering

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

type chatObservation struct {
	finished map[int]bool
}

type chatResponse struct {
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *chatUsage     `json:"usage"`
	Error jsontext.Value `json:"error"`
}

type chatUsage struct {
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	PromptDetails    *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (c *chatObservation) frame(o *observation, data []byte) {
	if o.failed || o.complete {
		return
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		o.complete = c.complete()
		o.failed = !o.complete
		return
	}
	var response chatResponse
	if json.Unmarshal(data, &response) != nil {
		o.failed = true
		return
	}
	if !o.streaming || len(response.Choices) == 0 {
		if cost := o.rule.Extract(data); cost != nil {
			o.cost = cost
		}
	}
	if response.Usage != nil {
		usage, valid := response.Usage.billingUsage()
		if !valid {
			o.failed = true
			return
		}
		o.usage = usage
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		o.cost = o.rule.Extract(data)
		o.failed = true
		return
	}
	for _, choice := range response.Choices {
		if choice.Index < 0 || choice.Index >= 1024 {
			o.failed = true
			return
		}
		if c.finished == nil {
			c.finished = make(map[int]bool)
		}
		switch choice.FinishReason {
		case "":
			if _, ok := c.finished[choice.Index]; !ok {
				c.finished[choice.Index] = false
			}
		case "stop", "length", "tool_calls", "function_call":
			c.finished[choice.Index] = true
		default:
			o.failed = true
			return
		}
	}
	if !o.streaming {
		o.complete = o.eof && c.complete()
	}
}

func (c *chatObservation) complete() bool {
	if len(c.finished) == 0 {
		return false
	}
	for _, finished := range c.finished {
		if !finished {
			return false
		}
	}
	return true
}

func (u *chatUsage) billingUsage() (billing.Usage, bool) {
	var cached *int64
	if u.PromptDetails != nil {
		cached = u.PromptDetails.CachedTokens
	}
	return billing.OpenAIUsage(u.PromptTokens, u.CompletionTokens, cached)
}
