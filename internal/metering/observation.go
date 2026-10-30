package metering

import (
	"bytes"
	"encoding/json/v2"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

type observation struct {
	rule      *billing.ResponseCostRule
	cost      *billing.ProviderCost
	usage     billing.Usage
	streaming bool
	failed    bool
	complete  bool
	buffer    []byte
}

type responseUsage struct {
	Type    string         `json:"type"`
	Usage   billing.Usage  `json:"usage"`
	Message *responseUsage `json:"message"`
}

func (o *observation) write(data []byte) {
	if o.failed || o.complete {
		return
	}
	if len(o.buffer)+len(data) > 4*1024*1024 {
		o.failed = true
		o.buffer = nil
		return
	}
	o.buffer = append(o.buffer, data...)
	if !o.streaming {
		return
	}
	for {
		line, rest, found := bytes.Cut(o.buffer, []byte{'\n'})
		if !found {
			return
		}
		o.buffer = rest
		raw, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
		if ok {
			o.frame(raw)
		}
	}
}

func (o *observation) frame(data []byte) {
	var event responseUsage
	if json.Unmarshal(data, &event) != nil {
		o.failed = true
		return
	}
	switch event.Type {
	case "message_start":
		if event.Message != nil {
			o.usage = event.Message.Usage
		}
	case "message_delta":
		mergeUsage(&o.usage, event.Usage)
	case "message_stop":
		if o.rule != nil && o.rule.StreamEvent == event.Type {
			o.cost = o.rule.Extract(data)
		}
		o.complete = true
	case "message":
		o.cost = o.rule.Extract(data)
		o.usage, o.complete = event.Usage, true
	case "error":
		o.failed = true
	}
}

func (o *observation) end() {
	if !o.streaming && !o.failed && !o.complete {
		o.frame(o.buffer)
	}
}

func mergeUsage(target *billing.Usage, next billing.Usage) {
	if next.InputTokens != nil {
		target.InputTokens = next.InputTokens
	}
	if next.OutputTokens != nil {
		target.OutputTokens = next.OutputTokens
	}
	if next.CacheReadTokens != nil {
		target.CacheReadTokens = next.CacheReadTokens
	}
	if next.CacheWriteTokens != nil {
		target.CacheWriteTokens = next.CacheWriteTokens
	}
	if next.CacheCreation != nil {
		target.CacheCreation = next.CacheCreation
	}
	if next.ServerToolUse != nil {
		target.ServerToolUse = next.ServerToolUse
	}
}
