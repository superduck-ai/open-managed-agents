package llmproviders

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestContextWindowsUseQualifiedRoutesAndWorkspacePrecedence(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{{Slug: "local", ModelIDs: []string{"same"}, ModelContextWindows: map[string]int{"same": 256000}}})
	catalog.organization = &db.LLMProvider{Slug: OrganizationSlug, ModelIDs: []string{"same", "shared"}, ModelContextWindows: map[string]int{"same": 1000000, "shared": 128000}}
	windows := catalog.ContextWindows()
	for id, want := range map[string]int{"local/same": 256000, "organization/same": 1000000, "same": 256000, "shared": 128000} {
		if windows[id] != want {
			t.Fatalf("%s window=%d want=%d", id, windows[id], want)
		}
	}
	catalog.providers = append(catalog.providers, db.LLMProvider{Slug: "other", ModelIDs: []string{"same"}, ModelContextWindows: map[string]int{"same": 64000}})
	if _, ok := catalog.ContextWindows()["same"]; ok {
		t.Fatal("ambiguous model received context window")
	}
}

func TestModelOptionsUseQualifiedRoutesAndWorkspacePrecedence(t *testing.T) {
	catalog := NewCatalog([]db.LLMProvider{{Slug: "local", ModelIDs: []string{"same"}, ModelOptions: map[string]db.ModelOptions{"same": {MaxOutputTokens: new(8000)}}}})
	catalog.organization = &db.LLMProvider{Slug: OrganizationSlug, ModelIDs: []string{"same"}, ModelOptions: map[string]db.ModelOptions{"same": {MaxOutputTokens: new(128000), InputModalities: []string{"text", "image"}}}}
	for id, want := range map[string]int{"local/same": 8000, "organization/same": 128000, "same": 8000} {
		got := catalog.ModelOptions()[id]
		if got.MaxOutputTokens == nil || *got.MaxOutputTokens != want {
			t.Fatalf("%s: %+v", id, got)
		}
	}
	catalog.providers = append(catalog.providers, db.LLMProvider{Slug: "other", ModelIDs: []string{"same"}})
	if _, ok := catalog.ModelOptions()["same"]; ok {
		t.Fatal("ambiguous model received options")
	}
}
