package billing

import (
	"maps"
	"slices"
	"strings"
)

type PriceOverride struct {
	MaxPromptTokens *int64   `json:"max_prompt_tokens,omitzero"`
	MinPromptTokens *int64   `json:"min_prompt_tokens,omitzero"`
	UTCStart        *int     `json:"utc_start,omitzero"`
	UTCEnd          *int     `json:"utc_end,omitzero"`
	UTCDays         []string `json:"utc_days,omitempty"`
	Rates           Rates    `json:"rates"`
}

func (p ModelPrice) effectiveRates(u TokenUsage) Rates {
	rates := *p.Rates
	total := u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
	at := p.PricedAt.UTC()
	clock := at.Hour()*100 + at.Minute()
	for _, o := range p.Overrides {
		if o.MaxPromptTokens != nil && total > *o.MaxPromptTokens {
			continue
		}
		if o.MinPromptTokens != nil && total <= *o.MinPromptTokens {
			continue
		}
		if len(o.UTCDays) > 0 && !slices.Contains(o.UTCDays, strings.ToLower(at.Weekday().String())) {
			continue
		}
		if o.UTCStart != nil && o.UTCEnd != nil {
			start, end := *o.UTCStart, *o.UTCEnd
			inside := clock >= start && clock < end
			if start > end {
				inside = clock >= start || clock < end
			}
			if !inside {
				continue
			}
		}
		rates = overlayRates(rates, o.Rates)
	}
	return rates
}

func overlayRates(base, override Rates) Rates {
	for _, pair := range []struct {
		target **string
		value  *string
	}{
		{&base.Input, override.Input}, {&base.Output, override.Output}, {&base.CacheRead, override.CacheRead},
		{&base.CacheWrite, override.CacheWrite}, {&base.CacheWrite5m, override.CacheWrite5m}, {&base.CacheWrite1h, override.CacheWrite1h},
	} {
		if pair.value != nil {
			*pair.target = pair.value
		}
	}
	return base
}

func PriceAliases(catalog map[string]ModelPrice) map[string]ModelPrice {
	result := maps.Clone(catalog)
	candidates := make(map[string][]ModelPrice)
	for id, p := range catalog {
		if strings.HasPrefix(id, "~") {
			continue
		}
		_, bare, ok := strings.Cut(id, "/")
		if ok {
			candidates[bare] = append(candidates[bare], p)
		}
	}
	for bare, prices := range candidates {
		if _, exact := result[bare]; !exact && len(prices) == 1 {
			result[bare] = prices[0]
		}
	}
	return result
}
