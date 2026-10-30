package billing

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math/big"
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
	raw := jsontext.Value(data)
	for part := range strings.SplitSeq(strings.TrimPrefix(r.AmountPointer, "/"), "/") {
		var fields map[string]jsontext.Value
		if json.Unmarshal(raw, &fields) != nil {
			return nil
		}
		raw = fields[strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")]
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
