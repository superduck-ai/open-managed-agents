package llmproviders

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/pricing"
)

const NarrowsSlug = "narrows"

type CatalogLoader func(context.Context, string, string) (Catalog, error)

func BillingCalculator(load CatalogLoader, prices *pricing.Store) *billing.Calculator {
	return billing.NewScopedCalculator(func(ctx context.Context, org, workspace string, requested []string) (map[string]billing.ModelPrice, error) {
		catalog, err := load(ctx, org, workspace)
		if err != nil && !errors.Is(err, ErrNotConfigured) {
			return nil, err
		}
		return providerPrices(catalog, prices, requested), nil
	})
}

func providerPrices(catalog Catalog, prices *pricing.Store, requested []string) map[string]billing.ModelPrice {
	models := make(map[string]billing.ModelPrice)
	now := time.Now().UTC()
	providers := catalog.providers
	if catalog.organization != nil {
		providers = append(slices.Clone(providers), *catalog.organization)
	}
	for _, provider := range providers {
		for _, model := range provider.Models.IDs() {
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
	for _, provider := range providers {
		for _, model := range provider.Models.IDs() {
			selected, err := catalog.Lookup(model)
			if err != nil {
				continue
			}
			if price, ok := models[selected.QualifiedModelID()]; ok {
				if _, exact := models[model]; !exact {
					models[model] = price
				}
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
