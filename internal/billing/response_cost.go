package billing

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

type ResponseCostRule struct {
	AmountPointer string `yaml:"amount_pointer" json:"amount_pointer"`
	StreamEvent   string `yaml:"stream_event" json:"stream_event"`
	Currency      string `yaml:"currency" json:"currency"`
	ExchangeRate  string `yaml:"exchange_rate" json:"exchange_rate"`
}

type ProviderCost struct {
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	ExchangeRate string `json:"exchange_rate"`
	USDCents     int64  `json:"usd_cents"`
}

func (r ResponseCostRule) Validate() error {
	if r.AmountPointer == "" || !jsontext.Pointer(r.AmountPointer).IsValid() || r.StreamEvent != "message_stop" {
		return errors.New("响应费用需要有效的 JSON Pointer 和 message_stop 结束事件")
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
