package billing

func (c *Calculator) PriceSnapshot(model string) *ModelPrice {
	if c == nil {
		return nil
	}
	price, ok := c.Models[normalizeModelName(model)]
	if !ok {
		return nil
	}
	return price.Clone()
}

func (p ModelPrice) Clone() *ModelPrice {
	result := p
	if p.Rates != nil {
		result.Rates = new(cloneRates(*p.Rates))
	}
	result.Overrides = make([]PriceOverride, len(p.Overrides))
	for i, rule := range p.Overrides {
		rule.Rates = cloneRates(rule.Rates)
		if rule.MinPromptTokens != nil {
			rule.MinPromptTokens = new(*rule.MinPromptTokens)
		}
		if rule.MaxPromptTokens != nil {
			rule.MaxPromptTokens = new(*rule.MaxPromptTokens)
		}
		if rule.UTCStart != nil {
			rule.UTCStart = new(*rule.UTCStart)
		}
		if rule.UTCEnd != nil {
			rule.UTCEnd = new(*rule.UTCEnd)
		}
		rule.UTCDays = append([]string(nil), rule.UTCDays...)
		result.Overrides[i] = rule
	}
	return &result
}

func cloneRates(r Rates) Rates {
	for _, field := range []**string{&r.Input, &r.Output, &r.CacheRead, &r.CacheWrite, &r.CacheWrite5m, &r.CacheWrite1h} {
		if *field != nil {
			*field = new(**field)
		}
	}
	return r
}
