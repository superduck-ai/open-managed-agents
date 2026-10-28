package metering

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

type chatObservation struct {
	event    []byte
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

func (c *chatObservation) line(o *observation, line []byte) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		if len(c.event) > 0 {
			c.frame(o, c.event)
			c.event = nil
		}
		return
	}
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	data = bytes.TrimPrefix(data, []byte{' '})
	if len(c.event)+len(data)+1 > 4*1024*1024 {
		o.failed = true
		c.event = nil
		return
	}
	c.event = append(c.event, data...)
	c.event = append(c.event, '\n')
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
	if response.Usage != nil {
		usage, valid := response.Usage.billingUsage()
		if !valid {
			o.failed = true
			return
		}
		o.usage = usage
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
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
	usage := billing.Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens}
	if (u.PromptTokens != nil && *u.PromptTokens < 0) || (u.CompletionTokens != nil && *u.CompletionTokens < 0) {
		return billing.Usage{}, false
	}
	if u.PromptDetails != nil && u.PromptDetails.CachedTokens != nil {
		cached := *u.PromptDetails.CachedTokens
		if cached < 0 || u.PromptTokens == nil || cached > *u.PromptTokens {
			return billing.Usage{}, false
		}
		usage.CacheReadTokens = u.PromptDetails.CachedTokens
		usage.InputTokens = new(*u.PromptTokens - cached)
	}
	return usage, true
}
