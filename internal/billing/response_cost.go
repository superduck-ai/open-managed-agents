package billing

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

type ResponseCostRule struct {
	Protocols     map[string]ResponseCostProtocol `yaml:"protocols" json:"protocols,omitempty"`
	AmountPointer string                          `yaml:"amount_pointer" json:"amount_pointer"`
	StreamEvent   string                          `yaml:"stream_event" json:"stream_event"`
	Currency      string                          `yaml:"currency" json:"currency"`
	ExchangeRate  string                          `yaml:"exchange_rate" json:"exchange_rate"`
}

type ResponseCostProtocol struct {
	NonStreaming *ResponseCostLocation `yaml:"non_streaming" json:"non_streaming,omitempty"`
	Streaming    *ResponseCostLocation `yaml:"streaming" json:"streaming,omitempty"`
}

type ResponseCostLocation struct {
	AmountPointer string   `yaml:"amount_pointer" json:"amount_pointer"`
	EventPointer  string   `yaml:"event_pointer" json:"event_pointer,omitempty"`
	Events        []string `yaml:"events" json:"events,omitempty"`
}

func (r *ResponseCostRule) ExtractFor(protocol string, streaming bool, data []byte) *ProviderCost {
	if r == nil {
		return nil
	}
	if len(r.Protocols) == 0 {
		if protocol != "messages" {
			return nil
		}
		if streaming && r.StreamEvent != "" {
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &event) != nil || event.Type != r.StreamEvent {
				return nil
			}
		}
		return r.Extract(data)
	}
	rule, ok := r.Protocols[protocol]
	if !ok {
		return nil
	}
	location := rule.NonStreaming
	if streaming {
		location = rule.Streaming
	}
	if location == nil {
		return nil
	}
	if len(location.Events) > 0 {
		raw, ok := pointerValue(jsontext.Value(data), strings.Split(strings.TrimPrefix(location.EventPointer, "/"), "/"))
		if !ok {
			return nil
		}
		var event string
		if json.Unmarshal(raw, &event) != nil || !slices.Contains(location.Events, event) {
			return nil
		}
	}
	selected := *r
	selected.AmountPointer = location.AmountPointer
	return selected.Extract(data)
}

type ProviderCost struct {
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	ExchangeRate string `json:"exchange_rate"`
	USDCents     int64  `json:"usd_cents"`
}

func (r ResponseCostRule) Validate() error {
	if len(r.Protocols) == 0 && (r.AmountPointer == "" || !jsontext.Pointer(r.AmountPointer).IsValid() || (r.StreamEvent != "" && r.StreamEvent != "message_stop")) {
		return errors.New("响应费用需要有效的 JSON Pointer；stream_event 仅兼容 message_stop，结束位置由协议确定")
	}
	for protocol, rule := range r.Protocols {
		if protocol != "messages" && protocol != "chat_completions" && protocol != "responses" {
			return errCostProtocolInvalid
		}
		if rule.NonStreaming == nil && rule.Streaming == nil {
			return errCostLocationMissing
		}
		for _, location := range []*ResponseCostLocation{rule.NonStreaming, rule.Streaming} {
			if location == nil {
				continue
			}
			if location.AmountPointer == "" || !jsontext.Pointer(location.AmountPointer).IsValid() {
				return errCostPointerInvalid
			}
			if (len(location.Events) > 0) != (location.EventPointer != "") || location.EventPointer != "" && !jsontext.Pointer(location.EventPointer).IsValid() {
				return errCostEventInvalid
			}
		}
	}
	if len(r.Protocols) > 0 && (r.AmountPointer != "" || r.StreamEvent != "") {
		return errCostConfigurationMixed
	}
	if r.Currency != "USD" && r.Currency != "CNY" {
		return errors.New("响应费用币种必须为 USD 或 CNY")
	}
	rate, ok := new(big.Rat).SetString(r.ExchangeRate)
	if !decimalRate.MatchString(r.ExchangeRate) || !ok || rate.Sign() <= 0 || (r.Currency == "USD" && r.ExchangeRate != "1") {
		return errors.New("响应费用需要正数汇率（每美元对应原币金额），USD 汇率必须为 1")
	}
	return nil
}

func (r *ResponseCostRule) Extract(data []byte) *ProviderCost {
	if r == nil {
		return nil
	}
	raw, ok := pointerValue(jsontext.Value(data), strings.Split(strings.TrimPrefix(r.AmountPointer, "/"), "/"))
	if !ok {
		return nil
	}
	var amount string
	if json.Unmarshal(raw, &amount) != nil || !decimalRate.MatchString(amount) {
		return nil
	}
	value, ok := new(big.Rat).SetString(amount)
	rate, valid := new(big.Rat).SetString(r.ExchangeRate)
	if !ok || !valid || rate.Sign() <= 0 {
		return nil
	}
	value.Quo(value, rate).Mul(value, big.NewRat(100, 1)).Add(value, big.NewRat(1, 2))
	cents := new(big.Int).Quo(value.Num(), value.Denom())
	if !cents.IsInt64() {
		return nil
	}
	return &ProviderCost{Amount: amount, Currency: r.Currency, ExchangeRate: r.ExchangeRate, USDCents: cents.Int64()}
}

func pointerValue(value jsontext.Value, tokens []string) (jsontext.Value, bool) {
	for _, token := range tokens {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		trimmed := bytes.TrimSpace(value)
		if len(trimmed) == 0 {
			return nil, false
		}
		switch trimmed[0] {
		case '{':
			var fields map[string]jsontext.Value
			if json.Unmarshal(value, &fields) != nil {
				return nil, false
			}
			next, exists := fields[token]
			if !exists {
				return nil, false
			}
			value = next
		case '[':
			var items []jsontext.Value
			if json.Unmarshal(value, &items) != nil {
				return nil, false
			}
			index, exists := pointerIndex(token, len(items))
			if !exists {
				return nil, false
			}
			value = items[index]
		default:
			return nil, false
		}
	}
	return value, true
}

func pointerIndex(token string, length int) (int, bool) {
	if token == "" || len(token) > 1 && token[0] == '0' {
		return 0, false
	}
	for _, digit := range token {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	index, err := strconv.Atoi(token)
	if err != nil || index >= length {
		return 0, false
	}
	return index, true
}

func (c *Calculator) ResponseCost(model string) *ResponseCostRule {
	if c == nil {
		return nil
	}
	slug, _, found := strings.Cut(model, "/")
	if !found {
		return nil
	}
	rule, found := c.ResponseCosts[slug]
	if !found {
		return nil
	}
	return &rule
}
