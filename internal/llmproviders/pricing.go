package llmproviders

import (
	"context"
	"slices"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/pricing"
)

const NarrowsSlug = "narrows"

type ProviderLoader func(context.Context, string, string) ([]db.LLMProvider, error)

func BillingCalculator(load ProviderLoader, prices *pricing.Store) *billing.Calculator {
	return billing.NewScopedCalculator(func(ctx context.Context, org, workspace string, requested []string) (map[string]billing.ModelPrice, error) {
		providers, err := load(ctx, org, workspace)
		if err != nil {
			return nil, err
		}
		return providerPrices(NewCatalog(providers), prices, requested), nil
	})
}

func providerPrices(catalog Catalog, prices *pricing.Store, requested []string) map[string]billing.ModelPrice {
	models := make(map[string]billing.ModelPrice)
	now := time.Now().UTC()
	for _, provider := range catalog.providers {
		for _, model := range provider.ModelIDs {
			qualified := QualifiedModelID(provider.Slug, model)
			if len(requested) > 0 && !slices.Contains(requested, qualified) && !slices.Contains(requested, model) {
				continue
			}
			price := prices.Lookup(priceSource(provider.Slug), model)
			if price == nil {
				continue
			}
			price.ProviderUUID = provider.UUID
			price.PricedAt = now
			models[qualified] = *price
		}
	}
	for _, provider := range catalog.providers {
		for _, model := range provider.ModelIDs {
			selected, err := catalog.Lookup(model)
			if err != nil {
				continue
			}
			if price, ok := models[selected.QualifiedModelID()]; ok {
				models[model] = price
			}
		}
	}
	return models
}

func priceSource(slug string) string {
	if slug == NarrowsSlug || slug == DefaultSlug {
		return "narrows"
	}
	return "openrouter"
}
