package sessions

import (
	"bytes"
	"encoding/json"
)

type sessionUsageAmount struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type sessionUsageBudget struct {
	Type        string              `json:"type"`
	MaxListCost *sessionUsageAmount `json:"max_list_cost"`
}

// decodeSessionUsageEvent validates the full-session snapshot without deriving
// counters, normalizing its JSON, or turning missing measurements into zeros.
func decodeSessionUsageEvent(raw json.RawMessage) (json.RawMessage, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, invalidSessionUsagePayload()
	}
	var eventType string
	if err := json.Unmarshal(event["type"], &eventType); err != nil || eventType != "session.usage" {
		return nil, invalidSessionUsagePayload()
	}
	for _, field := range []string{"thread_id", "session_thread_id", "owner_session_thread_id", "_owner_session_thread_id"} {
		if _, present := event[field]; present {
			return nil, invalidSessionUsagePayload()
		}
	}
	if !validSessionUsageSnapshot(event["usage"]) || !validSessionUsageBudget(event["budget"]) {
		return nil, invalidSessionUsagePayload()
	}
	return event["usage"], nil
}

func validSessionUsageSnapshot(raw json.RawMessage) bool {
	usage, ok := decodeSessionUsageObject(raw)
	if !ok {
		return false
	}
	cache, ok := decodeSessionUsageObject(usage["cache_creation"])
	if len(usage["cache_creation"]) > 0 && !ok {
		return false
	}
	tools, ok := decodeSessionUsageObject(usage["server_tool_use"])
	if len(usage["server_tool_use"]) > 0 && !ok {
		return false
	}
	for _, countRaw := range []json.RawMessage{
		usage["input_tokens"], usage["output_tokens"], usage["cache_read_input_tokens"],
		cache["ephemeral_1h_input_tokens"], cache["ephemeral_5m_input_tokens"],
		tools["web_search_requests"], tools["web_fetch_requests"],
	} {
		var count *int32
		if len(countRaw) > 0 && (json.Unmarshal(countRaw, &count) != nil || count == nil || *count < 0) {
			return false
		}
	}
	var seconds *float64
	if len(usage["active_seconds"]) > 0 && (json.Unmarshal(usage["active_seconds"], &seconds) != nil || seconds == nil || *seconds < 0) {
		return false
	}
	if len(usage["list_cost"]) > 0 {
		if _, ok := decodeSessionUsageAmount(usage["list_cost"]); !ok {
			return false
		}
	}
	return true
}

func validSessionUsageBudget(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return true
	}
	fields, ok := decodeSessionUsageObject(raw)
	if !ok {
		return false
	}
	var budget sessionUsageBudget
	if err := json.Unmarshal(fields["type"], &budget.Type); err != nil || budget.Type != "limit" {
		return false
	}
	budget.MaxListCost, ok = decodeSessionUsageAmount(fields["max_list_cost"])
	return ok && budget.MaxListCost.Amount != "0"
}

// Exact keys keep unknown case variants from overriding fields that are later
// persisted unchanged. Struct decoding alone would match keys case-insensitively.
func decodeSessionUsageObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	err := json.Unmarshal(raw, &fields)
	return fields, err == nil && fields != nil
}

func decodeSessionUsageAmount(raw json.RawMessage) (*sessionUsageAmount, bool) {
	fields, ok := decodeSessionUsageObject(raw)
	if !ok {
		return nil, false
	}
	var amount sessionUsageAmount
	if json.Unmarshal(fields["amount"], &amount.Amount) != nil || json.Unmarshal(fields["currency"], &amount.Currency) != nil {
		return nil, false
	}
	return &amount, amount.valid()
}

func (amount sessionUsageAmount) valid() bool {
	if amount.Currency != "USD" || amount.Amount == "" || (len(amount.Amount) > 1 && amount.Amount[0] == '0') {
		return false
	}
	for _, digit := range amount.Amount {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
