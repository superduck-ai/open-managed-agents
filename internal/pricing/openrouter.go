package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"math/big"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

const endpoint = "https://openrouter.ai/api/v1/models"

type tokenRates struct {
	Prompt     *string `json:"prompt"`
	Completion *string `json:"completion"`
	Read       *string `json:"input_cache_read"`
	Write      *string `json:"input_cache_write"`
	Write1h    *string `json:"input_cache_write_1h"`
}
type override struct {
	tokenRates
	MinPromptTokens *int64   `json:"min_prompt_tokens"`
	UTCStart        *int     `json:"utc_start"`
	UTCEnd          *int     `json:"utc_end"`
	UTCDays         []string `json:"utc_days"`
}
type sourcePrice struct {
	tokenRates
	Overrides []override `json:"overrides"`
}

func Parse(body []byte) ([]CatalogPrice, error) {
	var data struct {
		Data []struct {
			ID      string         `json:"id"`
			Pricing jsontext.Value `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	result := make([]CatalogPrice, 0, len(data.Data))
	for _, model := range data.Data {
		var source sourcePrice
		if err := json.Unmarshal(model.Pricing, &source); err != nil {
			return nil, err
		}
		rates, err := convertRates(model.ID, source.tokenRates)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(model.Pricing)
		price := billing.ModelPrice{Source: "openrouter", SourceModel: model.ID, Version: hex.EncodeToString(digest[:]), Rates: new(rates)}
		for _, o := range source.Overrides {
			if err := validateOverride(o); err != nil {
				return nil, err
			}
			rates, err := convertRates(model.ID, o.tokenRates)
			if err != nil {
				return nil, err
			}
			price.Overrides = append(price.Overrides, billing.PriceOverride{MinPromptTokens: o.MinPromptTokens, UTCStart: o.UTCStart, UTCEnd: o.UTCEnd, UTCDays: o.UTCDays, Rates: rates})
		}
		result = append(result, CatalogPrice{Model: model.ID, Price: price, Original: model.Pricing})
	}
	if len(result) == 0 {
		return nil, errors.New("OpenRouter 返回空价格目录")
	}
	return result, nil
}

func convertRates(model string, source tokenRates) (billing.Rates, error) {
	rates := billing.Rates{Input: source.Prompt, Output: source.Completion, CacheRead: source.Read, CacheWrite: source.Write, CacheWrite1h: source.Write1h}
	if strings.HasPrefix(model, "anthropic/") {
		rates.CacheWrite5m = rates.CacheWrite
		rates.CacheWrite = nil
	}
	for _, target := range []**string{&rates.Input, &rates.Output, &rates.CacheRead, &rates.CacheWrite, &rates.CacheWrite5m, &rates.CacheWrite1h} {
		if *target == nil {
			continue
		}
		if **target == "-1" {
			*target = nil
			continue
		}
		value, ok := new(big.Rat).SetString(**target)
		if !ok || value.Sign() < 0 {
			return billing.Rates{}, errors.New("OpenRouter 返回无效单价")
		}
		*target = new(value.Mul(value, big.NewRat(1000000, 1)).FloatString(12))
	}
	return rates, rates.Validate()
}

func validateOverride(o override) error {
	if o.MinPromptTokens != nil && *o.MinPromptTokens < 0 {
		return errors.New("无效的价格分档阈值")
	}
	if (o.UTCStart == nil) != (o.UTCEnd == nil) {
		return errors.New("价格时间窗口不完整")
	}
	for _, clock := range []*int{o.UTCStart, o.UTCEnd} {
		if clock != nil && (*clock < 0 || *clock > 2359 || *clock%100 > 59) {
			return errors.New("无效的价格时间窗口")
		}
	}
	if o.UTCStart != nil && *o.UTCStart == *o.UTCEnd {
		return errors.New("价格时间窗口为空")
	}
	for _, day := range o.UTCDays {
		switch day {
		case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		default:
			return errors.New("无效的价格星期条件")
		}
	}
	return nil
}
