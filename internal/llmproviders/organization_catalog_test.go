package llmproviders

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestOrganizationCatalogPreservesWorkspaceAmbiguity(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{
		{Slug: "first", Models: db.LLMModels{{ID: "shared"}}},
		{Slug: "second", Models: db.LLMModels{{ID: "shared"}}},
	})
	catalog.organization = []db.LLMProvider{{Slug: OrganizationSlug, Models: db.LLMModels{{ID: "shared"}}}}
	_, err := catalog.Lookup("shared")
	ambiguous, ok := errors.AsType[*AmbiguousModelError](err)
	if !ok || !slices.Equal(ambiguous.Candidates, []string{"first/shared", "second/shared"}) {
		t.Fatalf("Lookup(shared) = %v, want workspace ambiguity", err)
	}
	if _, err := catalog.Lookup("organization/missing"); !errors.Is(err, ErrModelNotConfigured) {
		t.Fatalf("Lookup(organization/missing) = %v", err)
	}
}

func TestOrganizationCatalogResolvesFallbackAndExplicitReferences(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{
		{Slug: "first", APIBackend: "messages", Models: db.LLMModels{{ID: "shared"}, {ID: "workspace-only"}}},
	})
	catalog.organization = []db.LLMProvider{{
		Slug: OrganizationSlug, APIBackend: "chat_completions", Models: db.LLMModels{{ID: "shared"}, {ID: "org-only"}, {ID: "vendor/model"}},
	}}
	for _, test := range []struct {
		modelID string
		slug    string
		model   string
		backend string
	}{
		{"shared", "first", "shared", "messages"},
		{"first/shared", "first", "shared", "messages"},
		{"organization/shared", OrganizationSlug, "shared", "chat_completions"},
		{"org-only", OrganizationSlug, "org-only", "chat_completions"},
		{"vendor/model", OrganizationSlug, "vendor/model", "chat_completions"},
		{"organization/vendor/model", OrganizationSlug, "vendor/model", "chat_completions"},
	} {
		t.Run(test.modelID, func(t *testing.T) {
			got, err := catalog.Lookup(test.modelID)
			if err != nil || got.Provider.Slug != test.slug || got.UpstreamModelID != test.model || got.Provider.APIBackend != test.backend {
				t.Fatalf("Lookup(%q) = (%+v, %v)", test.modelID, got, err)
			}
		})
	}
	want := []string{"first/shared", "first/workspace-only", "organization/shared", "organization/org-only", "organization/vendor/model"}
	if got := catalog.ModelIDs(); !slices.Equal(got, want) {
		t.Fatalf("ModelIDs() = %v, want %v", got, want)
	}
}

func TestOrganizationCatalogWorksWithoutWorkspaceProvider(t *testing.T) {
	catalog := NewCatalog(nil)
	catalog.organization = []db.LLMProvider{{Slug: OrganizationSlug, Models: db.LLMModels{{ID: "org-only"}}}}
	got, err := catalog.Lookup("org-only")
	if err != nil || got.QualifiedModelID() != "organization/org-only" {
		t.Fatalf("Lookup(org-only) = (%+v, %v)", got, err)
	}
}

func TestOrganizationCatalogBackendsPreserveWorkspacePriority(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{
		{Slug: "first", APIBackend: Messages, Models: db.LLMModels{{ID: "shared"}, {ID: "ambiguous"}}},
		{Slug: "second", APIBackend: ChatCompletions, Models: db.LLMModels{{ID: "ambiguous"}}},
	})
	catalog.organization = []db.LLMProvider{{Slug: OrganizationSlug, APIBackend: ChatCompletions, Models: db.LLMModels{{ID: "shared"}, {ID: "org-only"}, {ID: "ambiguous"}, {ID: "first/shared"}}}}
	backends, err := catalog.RuntimeModels()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"first/shared":              Messages,
		"first/ambiguous":           Messages,
		"second/ambiguous":          ChatCompletions,
		"shared":                    Messages,
		"organization/shared":       ChatCompletions,
		"organization/org-only":     ChatCompletions,
		"org-only":                  ChatCompletions,
		"organization/ambiguous":    ChatCompletions,
		"organization/first/shared": ChatCompletions,
	}
	if !maps.Equal(runtimeBackends(backends), want) {
		t.Fatalf("Backends() = %v, want %v", backends, want)
	}
}

func TestMultipleOrganizationProvidersRejectAmbiguousBareModels(t *testing.T) {
	catalog := NewCatalog(nil)
	catalog.organization = []db.LLMProvider{
		{Slug: "kimi", APIBackend: Messages, Models: db.LLMModels{{ID: "shared"}, {ID: "kimi-only"}}},
		{Slug: "deepseek", APIBackend: ChatCompletions, Models: db.LLMModels{{ID: "shared"}, {ID: "deepseek-only"}}},
	}
	if _, err := catalog.Lookup("shared"); !errors.Is(err, ErrAmbiguousModel) {
		t.Fatalf("ambiguous model=%v", err)
	}
	for _, id := range []string{"kimi/shared", "deepseek/shared", "kimi-only", "deepseek-only"} {
		if _, err := catalog.Lookup(id); err != nil {
			t.Fatalf("Lookup(%q)=%v", id, err)
		}
	}
	models, err := catalog.RuntimeModels()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := models["shared"]; ok {
		t.Fatal("ambiguous model exported to Brain")
	}
	if models["kimi/shared"].APIBackend != Messages || models["deepseek/shared"].APIBackend != ChatCompletions {
		t.Fatal("provider backend lost")
	}
}
