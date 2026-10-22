package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"math/big"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

const NarrowsCNYPerUSD = "7.2"
const narrowsEndpoint = "https://narrows.seewo.com/pricing/api/rules"

type narrowsComponent struct {
	Key      string `json:"key"`
	PerToken string `json:"per_token"`
}
type narrowsCondition struct {
	Greater *int64 `json:"input_tokens_gt"`
	AtLeast *int64 `json:"input_tokens_gte"`
	Maximum *int64 `json:"max_input_tokens"`
}
type narrowsRule struct {
	Model       string `json:"model"`
	Currency    string `json:"currency"`
	BillingType string `json:"billing_type"`
	Pricing     struct {
		Base  []narrowsComponent `json:"base"`
		Tiers []struct {
			Condition  narrowsCondition   `json:"condition"`
			Components []narrowsComponent `json:"components"`
		} `json:"tiers"`
	} `json:"pricing"`
}

func ParseNarrows(body []byte) ([]CatalogPrice, error) {
	var data struct {
		Rules []narrowsRule `json:"rules"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if len(data.Rules) == 0 {
		return nil, errors.New("Narrows 返回空价格目录")
	}
	result := make([]CatalogPrice, 0, len(data.Rules))
	seen := map[string]bool{}
	for _, rule := range data.Rules {
		if rule.Model == "" || seen[rule.Model] || rule.Currency != "CNY" || rule.BillingType != "llm" {
			return nil, errors.New("Narrows 模型、币种或计费类型无效")
		}
		seen[rule.Model] = true
		rates, err := narrowsRates(rule.Pricing.Base)
		if err != nil {
			return nil, err
		}
		original, err := json.Marshal(rule)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(append(original, []byte(NarrowsCNYPerUSD)...))
		price := billing.ModelPrice{Source: "narrows", SourceModel: rule.Model, SourceCurrency: "CNY", ExchangeRate: NarrowsCNYPerUSD, Version: hex.EncodeToString(digest[:]), Rates: new(rates)}
		for _, tier := range rule.Pricing.Tiers {
			lower, err := narrowsLowerBound(tier.Condition)
			if err != nil {
				return nil, err
			}
			rates, err := narrowsRates(tier.Components)
			if err != nil {
				return nil, err
			}
			price.Overrides = append(price.Overrides, billing.PriceOverride{MinPromptTokens: new(lower), MaxPromptTokens: tier.Condition.Maximum, Rates: rates})
		}
		result = append(result, CatalogPrice{Model: rule.Model, Price: price, Original: original})
	}
	return result, nil
}

func narrowsLowerBound(c narrowsCondition) (int64, error) {
	if (c.Greater == nil) == (c.AtLeast == nil) {
		return 0, errors.New("Narrows 分档下界无效")
	}
	var lower int64
	if c.Greater != nil {
		lower = *c.Greater
	} else {
		if *c.AtLeast < 1 {
			return 0, errors.New("Narrows 分档下界无效")
		}
		lower = *c.AtLeast - 1
	}
	if lower < 0 || (c.Maximum != nil && *c.Maximum <= lower) {
		return 0, errors.New("Narrows 分档区间无效")
	}
	return lower, nil
}

func narrowsRates(components []narrowsComponent) (billing.Rates, error) {
	var rates billing.Rates
	targets := map[string]**string{"input_cost_per_token": &rates.Input, "output_cost_per_token": &rates.Output, "cache_read_input_token_cost": &rates.CacheRead, "cache_creation_input_token_cost": &rates.CacheWrite}
	seen := map[string]bool{}
	ambiguousOutput, ambiguousCache := false, false
	for _, c := range components {
		if seen[c.Key] {
			return rates, errors.New("Narrows 重复价格字段")
		}
		seen[c.Key] = true
		value, ok := new(big.Rat).SetString(c.PerToken)
		if !ok || value.Sign() < 0 {
			return rates, errors.New("Narrows 返回无效单价")
		}
		if target, ok := targets[c.Key]; ok {
			*target = new(value.Mul(value, big.NewRat(1000000, 1)).FloatString(12))
			continue
		}
		switch c.Key {
		case "thinking_output_cost_per_token", "output_cost_per_image_token":
			ambiguousOutput = true
		case "implicit_cache_read_input_token_cost":
			ambiguousCache = true
		default:
			return rates, errors.New("Narrows 返回不支持的价格字段")
		}
	}
	if ambiguousOutput {
		rates.Output = nil
	}
	if ambiguousCache {
		rates.CacheRead = nil
	}
	return rates, rates.Validate()
}
