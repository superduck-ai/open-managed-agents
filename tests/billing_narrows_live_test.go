package tests

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math/big"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

type narrowsOracleComponent struct {
	Key      string `json:"key"`
	PerToken string `json:"per_token"`
}
type narrowsOracleRule struct {
	Model   string `json:"model"`
	Pricing struct {
		Base  []narrowsOracleComponent `json:"base"`
		Tiers []struct {
			Condition struct {
				GT  *int64 `json:"input_tokens_gt"`
				GTE *int64 `json:"input_tokens_gte"`
				Max *int64 `json:"max_input_tokens"`
			} `json:"condition"`
			Components []narrowsOracleComponent `json:"components"`
		} `json:"tiers"`
	} `json:"pricing"`
}

func reconcileNarrowsLive(result liveBillingResult, raw jsontext.Value, usage billing.TokenUsage) liveBillingResult {
	result.Status = "mismatch"
	var snapshot billing.ModelPrice
	if json.Unmarshal(result.PriceSnapshot, &snapshot) != nil || snapshot.Source != "narrows" || snapshot.SourceCurrency != "CNY" || snapshot.ExchangeRate != "7.2" || snapshot.SourceModel != result.Model {
		result.Detail = "Narrows 价格来源或汇率快照错误"
		return result
	}
	result.SourceModel = snapshot.SourceModel
	expected, ok := narrowsOracle(raw, usage)
	if !ok {
		if result.Cents != nil {
			result.Detail = "无法区分计费类别却产生金额"
			return result
		}
		result.Status = "unpriced"
		result.Detail = "返回用量无法匹配独立计费类别"
		return result
	}
	result.ExpectedCents = new(expected)
	if result.Cents == nil || *result.Cents != expected {
		result.Detail = "Narrows 原价除以 7.2 独立复算与落账不一致"
		return result
	}
	result.Status = "passed"
	return result
}

func narrowsOracle(raw jsontext.Value, u billing.TokenUsage) (int64, bool) {
	var rule narrowsOracleRule
	if json.Unmarshal(raw, &rule) != nil {
		return 0, false
	}
	rates := map[string]string{}
	for _, c := range rule.Pricing.Base {
		rates[c.Key] = c.PerToken
	}
	totalInput := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	for _, t := range rule.Pricing.Tiers {
		c := t.Condition
		if (c.GT != nil && totalInput <= *c.GT) || (c.GTE != nil && totalInput < *c.GTE) || (c.Max != nil && totalInput > *c.Max) {
			continue
		}
		for _, r := range t.Components {
			rates[r.Key] = r.PerToken
		}
	}
	if u.OutputTokens > 0 && (rates["thinking_output_cost_per_token"] != "" || rates["output_cost_per_image_token"] != "") {
		return 0, false
	}
	if u.CacheReadInputTokens > 0 && rates["implicit_cache_read_input_token_cost"] != "" {
		return 0, false
	}
	if u.CacheWrite5mTokens+u.CacheWrite1hTokens > 0 {
		return 0, false
	}
	total := new(big.Rat)
	for _, term := range []struct {
		key    string
		tokens int64
	}{{"input_cost_per_token", u.InputTokens}, {"output_cost_per_token", u.OutputTokens}, {"cache_read_input_token_cost", u.CacheReadInputTokens}, {"cache_creation_input_token_cost", u.CacheCreationInputTokens}} {
		if term.tokens == 0 {
			continue
		}
		r, ok := new(big.Rat).SetString(rates[term.key])
		if !ok || r.Sign() < 0 {
			return 0, false
		}
		total.Add(total, new(big.Rat).Mul(r, new(big.Rat).SetInt64(term.tokens)))
	}
	total.Mul(total, big.NewRat(125, 9)).Add(total, big.NewRat(1, 2))
	rounded := new(big.Int).Quo(total.Num(), total.Denom())
	return rounded.Int64(), rounded.IsInt64()
}

func TestNarrowsLiveOracle(t *testing.T) {
	raw := jsontext.Value(`{"model":"x","pricing":{"base":[{"key":"input_cost_per_token","per_token":"0.0000072"},{"key":"output_cost_per_token","per_token":"0.0000144"},{"key":"cache_read_input_token_cost","per_token":"0.00000072"},{"key":"cache_creation_input_token_cost","per_token":"0.000009"}],"tiers":[]}}`)
	usage := billing.TokenUsage{InputTokens: 100000, OutputTokens: 100000, CacheReadInputTokens: 100000, CacheCreationInputTokens: 100000}
	if got, ok := narrowsOracle(raw, usage); !ok || got != 44 {
		t.Fatalf("独立复算: %d/%v", got, ok)
	}
}
