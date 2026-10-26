package llmproviders

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestRuntimeModelsPreserveSettingsAndWorkspacePrecedence(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{{Slug: "local", APIBackend: Messages, Models: db.LLMModels{{ID: "same", ContextWindow: new(256000), MaxOutputTokens: new(8000)}}}})
	catalog.organization = &db.LLMProvider{Slug: OrganizationSlug, APIBackend: ChatCompletions, Models: db.LLMModels{{ID: "same", ContextWindow: new(1000000), MaxOutputTokens: new(128000), InputModalities: []string{"text", "image"}}, {ID: "shared"}}}
	models, err := catalog.RuntimeModels()
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"local/same": 256000, "organization/same": 1000000, "same": 256000} {
		if model := models[id]; model.ContextWindow == nil || *model.ContextWindow != want {
			t.Fatalf("%s: %+v", id, model)
		}
	}
	if *models["same"].MaxOutputTokens != 8000 || models["same"].APIBackend != Messages || len(models["organization/same"].InputModalities) != 2 {
		t.Fatalf("settings: %+v", models)
	}
	if _, ok := models["shared"]; !ok {
		t.Fatal("model without parameters disappeared")
	}
	catalog.providers = append(catalog.providers, db.LLMProvider{Slug: "other", APIBackend: Messages, Models: db.LLMModels{{ID: "same"}}})
	models, err = catalog.RuntimeModels()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := models["same"]; ok {
		t.Fatal("ambiguous model received an alias")
	}
}
