package billing

func (c *Calculator) PriceSnapshot(model string) *ModelPrice {
	if c == nil {
		return nil
	}
	price, ok := c.Models[normalizeModelName(model)]
	if !ok {
		return nil
	}
	return new(price)
}
