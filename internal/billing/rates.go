package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

type Rates struct {
	Input        *string `json:"input"`
	Output       *string `json:"output"`
	CacheRead    *string `json:"cache_read"`
	CacheWrite   *string `json:"cache_write"`
	CacheWrite5m *string `json:"cache_write_5m"`
	CacheWrite1h *string `json:"cache_write_1h"`
}

var decimalRate = regexp.MustCompile(`^(0|[1-9][0-9]{0,11})(\.[0-9]{1,12})?$`)

func (r Rates) Validate() error {
	for _, value := range []*string{r.Input, r.Output, r.CacheRead, r.CacheWrite, r.CacheWrite5m, r.CacheWrite1h} {
		if value != nil && !decimalRate.MatchString(*value) {
			return errors.New("价格必须为非负十进制数，最多12位小数")
		}
	}
	return nil
}

func (r Rates) Cost(u TokenUsage) (int64, bool) {
	return r.cost(u, "")
}

func (r Rates) cost(u TokenUsage, exchangeRate string) (int64, bool) {
	cents, ok := r.cents(u, exchangeRate)
	if !ok {
		return 0, false
	}
	rounded := scaleHalfUp(cents, 1)
	return rounded.Int64(), rounded.IsInt64()
}

func (r Rates) cents(u TokenUsage, exchangeRate string) (*big.Rat, bool) {
	if u.CacheWrite5mTokens+u.CacheWrite1hTokens > u.CacheCreationInputTokens {
		return nil, false
	}
	total := new(big.Rat)
	terms := []struct {
		tokens int64
		rate   *string
	}{
		{u.InputTokens, r.Input}, {u.OutputTokens, r.Output}, {u.CacheReadInputTokens, r.CacheRead},
		{u.CacheWrite5mTokens, r.CacheWrite5m}, {u.CacheWrite1hTokens, r.CacheWrite1h},
		{u.CacheCreationInputTokens - u.CacheWrite5mTokens - u.CacheWrite1hTokens, r.CacheWrite},
	}
	for _, term := range terms {
		if term.tokens < 0 {
			return nil, false
		}
		if term.tokens == 0 {
			continue
		}
		if term.rate == nil {
			return nil, false
		}
		rate, ok := new(big.Rat).SetString(*term.rate)
		if !ok || rate.Sign() < 0 {
			return nil, false
		}
		total.Add(total, new(big.Rat).Mul(rate, new(big.Rat).SetInt64(term.tokens)))
	}
	if exchangeRate != "" {
		rate, ok := new(big.Rat).SetString(exchangeRate)
		if !ok || rate.Sign() <= 0 {
			return nil, false
		}
		total.Quo(total, rate)
	}
	total.Quo(total, big.NewRat(10000, 1))
	return total, true
}

type PriceLoader func(context.Context, string, string, []string) (map[string]ModelPrice, error)

func NewDatabaseCalculator(loader PriceLoader) *Calculator { return &Calculator{loader: loader} }

func (c *Calculator) Scope(ctx context.Context, org, workspace string, models ...string) (*Calculator, error) {
	if c == nil || c.loader == nil {
		return c, nil
	}
	prices, err := c.loader(ctx, org, workspace, models)
	if err != nil {
		return nil, err
	}
	return NewCalculator(prices), nil
}

func (c *Calculator) ValidateSnapshot(ctx context.Context, org, workspace string, snapshot json.RawMessage) error {
	scoped, err := c.Scope(ctx, org, workspace)
	if err != nil {
		return err
	}
	if missing := scoped.UnpricedSnapshotModels(snapshot); len(missing) > 0 {
		return fmt.Errorf("budget requires models with a list price; no list price configured for: %s", strings.Join(missing, ", "))
	}
	return nil
}
