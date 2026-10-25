package llmproviders

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/pricing"
)

func TestProviderPriceSourcesAndIsolation(t *testing.T) {
	store := pricing.NewStore(config.PricingConfig{SnapshotFile: filepath.Join(t.TempDir(), "prices.json")}, nil)
	providers := []db.LLMProvider{
		{UUID: "n", Slug: "narrows", BaseURL: "https://arbitrary.example", Models: db.LLMModels{{ID: "model"}}},
		{UUID: "d", Slug: "default", Models: db.LLMModels{{ID: "model"}}},
		{UUID: "o", Slug: "default-2", BaseURL: "https://narrows-gateway.test.seewo.com", Models: db.LLMModels{{ID: "model"}}},
	}
	calculator := BillingCalculator(func(_ context.Context, org, workspace string) (Catalog, error) {
		if org != "org" || workspace != "workspace" {
			return Catalog{}, nil
		}
		return NewCatalog(providers), nil
	}, store)
	open, err := pricing.Parse([]byte(`{"data":[{"id":"model","pricing":{"prompt":"0.000001","completion":"0"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Replace("openrouter", open); err != nil {
		t.Fatal(err)
	}
	scoped, err := calculator.Scope(t.Context(), "org", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if scoped.HasModel("narrows/model") || scoped.HasModel("default/model") || scoped.HasModel("model") {
		t.Fatal("缺价回退或歧义模型被计价")
	}
	narrow := open[0]
	narrow.Price = *narrow.Price.Clone()
	narrow.Price.Source = "narrows"
	narrow.Price.SourceCurrency = "CNY"
	narrow.Price.ExchangeRate = "7.2"
	narrow.Price.Rates.Input = new("14.4")
	if err := store.Replace("narrows", []pricing.CatalogPrice{narrow}); err != nil {
		t.Fatal(err)
	}
	scoped, err = calculator.Scope(t.Context(), "org", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	for model, cents := range map[string]int64{"narrows/model": 200, "default/model": 200, "default-2/model": 100} {
		got, ok := scoped.ModelRequestCents(model, billing.TokenUsage{InputTokens: 1000000})
		if !ok || got != cents {
			t.Fatalf("%s: %d/%v", model, got, ok)
		}
	}
	other, err := calculator.Scope(t.Context(), "other", "workspace")
	if err != nil || other.HasModel("narrows/model") {
		t.Fatal("跨组织价格泄漏", err)
	}
	selected, err := calculator.Scope(t.Context(), "org", "workspace", "narrows/model")
	if err != nil || selected.HasModel("default/model") || selected.HasModel("model") {
		t.Fatal("加载了未选择或歧义模型", err)
	}
	if selected.PriceSnapshot("narrows/model").ProviderUUID != "n" {
		t.Fatal("供应商身份丢失")
	}
}

func TestOrganizationNarrowsPriceSource(t *testing.T) {
	store := pricing.NewStore(config.PricingConfig{SnapshotFile: filepath.Join(t.TempDir(), "prices.json")}, nil)
	open, err := pricing.Parse([]byte(`{"data":[{"id":"shared","pricing":{"prompt":"0.000001","completion":"0"}},{"id":"open-only","pricing":{"prompt":"0.000001","completion":"0"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Replace("openrouter", open); err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog(nil)
	catalog.organization = &db.LLMProvider{UUID: "organization-provider", Slug: OrganizationSlug, BaseURL: "https://narrows-gateway.test.seewo.com:443/v1", Models: db.LLMModels{{ID: "narrows-only"}, {ID: "shared"}, {ID: "open-only"}}}
	calculator := BillingCalculator(func(context.Context, string, string) (Catalog, error) { return catalog, nil }, store)
	scoped, err := calculator.Scope(t.Context(), "org", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	if scoped.HasModel("organization/shared") || scoped.HasModel("open-only") {
		t.Fatal("organization Narrows provider fell back to OpenRouter")
	}
	var narrows []pricing.CatalogPrice
	for _, model := range []string{"narrows-only", "shared"} {
		narrows = append(narrows, pricing.CatalogPrice{Model: model, Original: []byte(`{}`), Price: billing.ModelPrice{
			Source: "narrows", SourceModel: model, SourceCurrency: "CNY", ExchangeRate: "7.2", Version: "fixture",
			Rates: &billing.Rates{Input: new("14.4"), Output: new("0")},
		}})
	}
	if err := store.Replace("narrows", narrows); err != nil {
		t.Fatal(err)
	}
	scoped, err = calculator.Scope(t.Context(), "org", "workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"narrows-only", "organization/narrows-only", "shared", "organization/shared"} {
		price := scoped.PriceSnapshot(model)
		cents, priced := scoped.BudgetRequestCents(model, billing.TokenUsage{InputTokens: 1000000})
		if !scoped.HasModel(model) || !priced || cents != 200 || price == nil || price.Source != "narrows" || price.ProviderUUID != "organization-provider" {
			t.Fatalf("%s: price=%+v cost=%d/%t", model, price, cents, priced)
		}
	}
	if err := scoped.ValidateSnapshot(t.Context(), "org", "workspace", []byte(`{"model":{"id":"organization/narrows-only"}}`)); err != nil {
		t.Fatalf("Narrows-only organization model rejected by budget validation: %v", err)
	}
	if scoped.HasModel("open-only") {
		t.Fatal("missing Narrows price fell back to OpenRouter")
	}
	for _, baseURL := range []string{"https://provider.example/v1", "https://narrows-gateway.test.seewo.com.other.example/v1"} {
		catalog.organization.BaseURL = baseURL
		other, err := calculator.Scope(t.Context(), "org", "workspace")
		if err != nil {
			t.Fatal(err)
		}
		price := other.PriceSnapshot("organization/shared")
		if price == nil || price.Source != "openrouter" || other.HasModel("narrows-only") {
			t.Fatalf("ordinary organization provider selected Narrows: %+v", price)
		}
	}
}

func TestOrganizationPricesFollowModelRouting(t *testing.T) {
	store := pricing.NewStore(config.PricingConfig{SnapshotFile: filepath.Join(t.TempDir(), "prices.json")}, nil)
	entries, err := pricing.Parse([]byte(`{"data":[{"id":"model","pricing":{"prompt":"0.000003","completion":"0"}},{"id":"other","pricing":{"prompt":"0.000001","completion":"0"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Replace("openrouter", entries); err != nil {
		t.Fatal(err)
	}
	organization := db.LLMProvider{UUID: "org-provider", Slug: OrganizationSlug, Models: db.LLMModels{{ID: "model"}, {ID: "other"}}}
	for _, test := range []struct {
		name      string
		providers []db.LLMProvider
		want      string
	}{
		{"unpriced workspace never falls back", []db.LLMProvider{{UUID: "workspace", Slug: NarrowsSlug, Models: db.LLMModels{{ID: "model"}}}}, ""},
		{"ambiguous workspace never falls back", []db.LLMProvider{{UUID: "first", Slug: "first", Models: db.LLMModels{{ID: "model"}}}, {UUID: "second", Slug: "second", Models: db.LLMModels{{ID: "model"}}}}, ""},
		{"workspace overrides organization", []db.LLMProvider{{UUID: "workspace", Slug: "workspace", Models: db.LLMModels{{ID: "model"}}}}, "workspace"},
		{"organization works alone", nil, "org-provider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := NewCatalog(test.providers)
			catalog.organization = &organization
			calculator := BillingCalculator(func(_ context.Context, org, _ string) (Catalog, error) {
				if org != "org" {
					return Catalog{}, ErrNotConfigured
				}
				return catalog, nil
			}, store)
			scoped, err := calculator.Scope(t.Context(), "org", "workspace", "model", "organization/model", "other")
			if err != nil {
				t.Fatal(err)
			}
			price := scoped.PriceSnapshot("model")
			if test.want == "" && price != nil || test.want != "" && (price == nil || price.ProviderUUID != test.want) {
				t.Fatalf("bare model price=%+v, want provider %q", price, test.want)
			}
			for _, model := range []string{"organization/model", "other"} {
				price := scoped.PriceSnapshot(model)
				if price == nil || price.ProviderUUID != organization.UUID {
					t.Fatalf("%s lost organization price: %+v", model, price)
				}
			}
			other, err := calculator.Scope(t.Context(), "other-org", "workspace", "organization/model")
			if err != nil || other.HasModel("organization/model") {
				t.Fatalf("organization price leaked: %v", err)
			}
		})
	}
}
