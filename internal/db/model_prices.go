package db

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/url"
	"slices"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/yourbatis"
)

type CatalogPrice struct {
	Model    string
	Price    billing.ModelPrice
	Original jsontext.Value
}

func (d *DB) ReplaceModelPriceCatalog(ctx context.Context, source string, prices []CatalogPrice) error {
	if len(prices) == 0 {
		return errors.New("价格目录为空，保留已有价格")
	}
	rows := make([]modelPriceCatalogWrite, 0, len(prices))
	for _, p := range prices {
		if p.Model == "" || p.Price.Rates == nil {
			return errors.New("价格目录条目无效")
		}
		if err := p.Price.Rates.Validate(); err != nil {
			return err
		}
		value, err := json.Marshal(p.Price)
		if err != nil {
			return err
		}
		rows = append(rows, modelPriceCatalogWrite{Source: source, Model: p.Model, Price: value, Original: p.Original})
	}
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewModelPriceCatalogMapper(executor)
		if err := mapper.DeleteSource(ctx, source); err != nil {
			return err
		}
		for _, row := range rows {
			if err := mapper.Save(ctx, row); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) BillingCalculator() *billing.Calculator {
	return billing.NewDatabaseCalculator(func(ctx context.Context, org, workspace string, requested []string) (map[string]billing.ModelPrice, error) {
		providers, err := d.ListLLMProviders(ctx, org, workspace)
		if err != nil {
			return nil, err
		}
		models := make(map[string]billing.ModelPrice)
		now := time.Now().UTC()
		for _, provider := range providers {
			modelIDs := []string{}
			for _, model := range provider.ModelIDs {
				if len(requested) == 0 || slices.Contains(requested, model) {
					modelIDs = append(modelIDs, model)
				}
			}
			if len(modelIDs) == 0 {
				continue
			}
			source := "openrouter"
			parsed, err := url.Parse(provider.BaseURL)
			if err != nil {
				return nil, err
			}
			if parsed.Hostname() == "narrows-gateway.test.seewo.com" {
				source = "narrows"
			}
			rows, err := NewModelPriceCatalogMapper(d.mapperDB).Find(ctx, source, modelIDs)
			if err != nil {
				return nil, err
			}
			catalog := make(map[string]billing.ModelPrice, len(rows))
			for _, row := range rows {
				var p billing.ModelPrice
				if err := json.Unmarshal(row.Price, &p); err != nil {
					return nil, err
				}
				catalog[row.Model] = p
			}
			if source == "openrouter" {
				catalog = billing.PriceAliases(catalog)
			}
			for _, model := range modelIDs {
				if p, ok := catalog[model]; ok {
					p.ProviderUUID = provider.UUID
					p.PricedAt = now
					models[model] = p
				}
			}
		}
		return models, nil
	})
}
