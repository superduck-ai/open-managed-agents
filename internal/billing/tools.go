package billing

import (
	"math/big"
	"strconv"
	"strings"
)

const cnyPerUSD = "7.2"

type billableTool struct {
	cny  string
	per  int64
	uses func(Usage) int64
}

var billableTools = []billableTool{
	{cny: "5", per: 1000, uses: Usage.Searches},
}

type SearchPrice struct {
	CNY      string `json:"cny"`
	Per      int64  `json:"per"`
	Exchange string `json:"exchange"`
}

func CurrentSearchPrice() SearchPrice {
	for _, tool := range billableTools {
		if tool.uses != nil {
			return SearchPrice{CNY: tool.cny, Per: tool.per, Exchange: cnyPerUSD}
		}
	}
	return LegacySearchPrice()
}

func LegacySearchPrice() SearchPrice {
	return SearchPrice{CNY: "5", Per: 1000, Exchange: "7.2"}
}

func ResolveSearchPrice(cny, per, exchange string) SearchPrice {
	count, err := strconv.ParseInt(per, 10, 64)
	if cny == "" || exchange == "" || err != nil || count <= 0 {
		return LegacySearchPrice()
	}
	return SearchPrice{CNY: cny, Per: count, Exchange: exchange}
}

func (p SearchPrice) Dollars(count int64) *big.Rat {
	total := new(big.Rat)
	if count <= 0 || p.Per <= 0 {
		return total
	}
	rate, ok := new(big.Rat).SetString(p.Exchange)
	price, priceOK := new(big.Rat).SetString(p.CNY)
	if !ok || !priceOK || rate.Sign() <= 0 || price.Sign() < 0 {
		return total
	}
	part := new(big.Rat).SetInt64(count)
	part.Mul(part, price)
	part.Quo(part, new(big.Rat).SetInt64(p.Per))
	part.Quo(part, rate)
	return part
}

func ToolCostCents(usage Usage) int64 {
	return DollarsToCents(ToolDollars(usage))
}

func DollarsToCents(dollars *big.Rat) int64 {
	return roundDollarsToCents(dollars)
}

func FormatDollars(amount *big.Rat) string {
	raw := scaleHalfUp(amount, 1_000_000).String()
	if len(raw) <= 6 {
		raw = strings.Repeat("0", 7-len(raw)) + raw
	}
	split := len(raw) - 6
	return raw[:split] + "." + raw[split:]
}

func ToolDollars(usage Usage) *big.Rat {
	total := new(big.Rat)
	for _, tool := range billableTools {
		if tool.uses == nil {
			continue
		}
		total.Add(total, (SearchPrice{CNY: tool.cny, Per: tool.per, Exchange: cnyPerUSD}).Dollars(tool.uses(usage)))
	}
	return total
}

func roundDollarsToCents(dollars *big.Rat) int64 {
	return scaleHalfUp(dollars, 100).Int64()
}

func scaleHalfUp(amount *big.Rat, scale int64) *big.Int {
	if amount == nil || amount.Sign() <= 0 {
		return new(big.Int)
	}
	scaled := new(big.Rat).Mul(amount, big.NewRat(scale, 1))
	scaled.Add(scaled, big.NewRat(1, 2))
	return new(big.Int).Quo(scaled.Num(), scaled.Denom())
}
