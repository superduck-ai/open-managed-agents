package dreams

import (
	"encoding/json"
	"strconv"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// dreamTokenUsage is the four-count Anthropic Dream usage object persisted on
// dreams.usage. Extra Session fields never enter this contract.
type dreamTokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

func (usage dreamTokenUsage) isZero() bool {
	return usage == dreamTokenUsage{}
}

func marshalDreamUsage(usage dreamTokenUsage) json.RawMessage {
	raw, err := json.Marshal(usage)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

// aggregateDreamUsage prefers the latest model-request totals, then unique
// agent.message turn usage, then the Session usage column. The Session column
// is often still `{}` while the events already carry per-turn counts.
func aggregateDreamUsage(events []db.SessionEvent, sessionUsage json.RawMessage) json.RawMessage {
	if usage, ok := latestModelRequestEndUsage(events); ok {
		return marshalDreamUsage(usage)
	}
	if usage, ok := sumAgentMessageUsage(events); ok {
		return marshalDreamUsage(usage)
	}
	if usage, ok := parseDreamUsage(sessionUsage); ok {
		return marshalDreamUsage(usage)
	}
	return json.RawMessage(`{}`)
}

func dreamUsageUnchanged(current json.RawMessage, next json.RawMessage) bool {
	left, _ := parseDreamUsage(current)
	right, _ := parseDreamUsage(next)
	return left == right
}

func latestModelRequestEndUsage(events []db.SessionEvent) (dreamTokenUsage, bool) {
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		if event.EventType != "span.model_request_end" {
			continue
		}
		if usage, ok := usageFromEventPayload(event.Payload); ok {
			return usage, true
		}
	}
	return dreamTokenUsage{}, false
}

func sumAgentMessageUsage(events []db.SessionEvent) (dreamTokenUsage, bool) {
	seen := map[string]struct{}{}
	var total dreamTokenUsage
	found := false
	anonymous := 0
	for _, event := range events {
		if event.EventType != "agent.message" {
			continue
		}
		usage, messageID, ok := agentMessageUsage(event.Payload)
		if !ok {
			continue
		}
		key := messageID
		if key == "" {
			anonymous++
			key = "anonymous:" + strconv.Itoa(anonymous)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.CacheReadInputTokens += usage.CacheReadInputTokens
		total.CacheCreationInputTokens += usage.CacheCreationInputTokens
		found = true
	}
	return total, found && !total.isZero()
}

func agentMessageUsage(payload json.RawMessage) (dreamTokenUsage, string, bool) {
	var envelope struct {
		ID      string `json:"id"`
		Message struct {
			ID    string          `json:"id"`
			Usage json.RawMessage `json:"usage"`
		} `json:"message"`
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return dreamTokenUsage{}, "", false
	}
	raw := envelope.Message.Usage
	if len(raw) == 0 {
		raw = envelope.Usage
	}
	usage, ok := parseDreamUsage(raw)
	if !ok {
		return dreamTokenUsage{}, "", false
	}
	messageID := envelope.Message.ID
	if messageID == "" {
		messageID = envelope.ID
	}
	return usage, messageID, true
}

func usageFromEventPayload(payload json.RawMessage) (dreamTokenUsage, bool) {
	var envelope struct {
		Usage      json.RawMessage `json:"usage"`
		ModelUsage json.RawMessage `json:"model_usage"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return dreamTokenUsage{}, false
	}
	if usage, ok := parseDreamUsage(envelope.Usage); ok {
		return usage, true
	}
	return parseDreamUsage(envelope.ModelUsage)
}

func parseDreamUsage(raw json.RawMessage) (dreamTokenUsage, bool) {
	if len(raw) == 0 {
		return dreamTokenUsage{}, false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return dreamTokenUsage{}, false
	}
	usage := dreamTokenUsage{
		InputTokens:              tokenCount(object, "input_tokens", "inputTokens"),
		OutputTokens:             tokenCount(object, "output_tokens", "outputTokens"),
		CacheReadInputTokens:     tokenCount(object, "cache_read_input_tokens", "cacheReadInputTokens"),
		CacheCreationInputTokens: tokenCount(object, "cache_creation_input_tokens", "cacheCreationInputTokens"),
	}
	return usage, !usage.isZero()
}

func tokenCount(object map[string]json.RawMessage, keys ...string) int64 {
	for _, key := range keys {
		raw, ok := object[key]
		if !ok {
			continue
		}
		var value float64
		if json.Unmarshal(raw, &value) != nil {
			continue
		}
		return int64(value)
	}
	return 0
}

func containsModelRequestEnd(events []db.SessionEvent) bool {
	return lastModelRequestSpan(events) == "span.model_request_end"
}

func lastModelRequestSpan(events []db.SessionEvent) string {
	last := ""
	for _, event := range events {
		switch event.EventType {
		case "span.model_request_start", "span.model_request_end":
			last = event.EventType
		}
	}
	return last
}

// dreamSessionFinished requires a true end-of-run idle: Session is idle and
// the latest model span is an end. Tool-turn idles that only have a start, or
// that have already started the next request, stay running.
func dreamSessionFinished(session db.Session, events []db.SessionEvent) bool {
	return session.Status == "idle" && containsModelRequestEnd(events)
}
