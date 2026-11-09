//go:build integration

package devinit

import (
	"errors"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func testProviderRecovery(t *testing.T, database *db.DB, vault *secrets.Service, layout Layout, state *State) {
	t.Helper()
	providers := ModelProvidersSpec{Providers: []ProviderSpec{
		{Name: "first", APIBackend: "messages", BaseURL: "https://api.anthropic.com", APIKey: "first-key", ModelIDs: []string{"shared-model"}},
		{Name: "second", Slug: "compatible", APIBackend: "chat_completions", BaseURL: "https://provider.example", APIKey: "second-key", ModelIDs: []string{"shared-model"}},
	}}
	partial := providers.Providers[0]
	partial.Slug = "default"
	committed, err := createProvider(t.Context(), database, vault, state.Organization.UUID, state.Workspaces[0], partial)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := reconcileProviders(t.Context(), database, vault, providers, state, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if state.Providers[0].ExternalID != committed.ExternalID {
		t.Fatal("retry replaced the committed provider")
	}
	if err := SaveState(layout, *state); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err := LoadState(layout.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	*state = reloaded
	original, err := database.ListOrganizationLLMProviders(t.Context(), state.Organization.UUID)
	if err != nil || len(original) != 4 {
		t.Fatalf("organization providers: %d, %v", len(original), err)
	}
	for index, workspace := range state.Workspaces {
		items, err := database.ListLLMProviders(t.Context(), state.Organization.UUID, workspace.UUID)
		if err != nil || len(items) != 2 {
			t.Fatalf("workspace providers: %d, %v", len(items), err)
		}
		suffix := ""
		if index == 1 {
			suffix = "-2"
		}
		byName := map[string]string{}
		for _, item := range items {
			byName[item.Name] = item.Slug
		}
		if byName["first"] != "default"+suffix || byName["second"] != "compatible"+suffix {
			t.Fatalf("provider slugs: %v", byName)
		}
	}
	changedSlug := ModelProvidersSpec{Providers: append([]ProviderSpec(nil), providers.Providers...)}
	changedSlug.Providers[1].Slug = "renamed"
	if err := reconcileProviders(t.Context(), database, vault, changedSlug, state, Options{Update: true}); err == nil {
		t.Fatal("immutable init slug change was accepted")
	}
	for i := range state.Providers {
		state.Providers[i].InputSlug = nil
	}
	if err := SaveState(layout, *state); err != nil {
		t.Fatal(err)
	}
	reloaded, _, err = LoadState(layout.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	*state = reloaded
	if err := reconcileProviders(t.Context(), database, vault, providers, state, Options{}); err != nil {
		t.Fatalf("legacy state with migration-assigned slugs: %v", err)
	}
	unchanged, err := database.ListOrganizationLLMProviders(t.Context(), state.Organization.UUID)
	if err != nil || !reflect.DeepEqual(original, unchanged) {
		t.Fatalf("legacy retry changed providers: %v", err)
	}
	providers.Providers[1].BaseURL = "https://updated.example"
	providers.Providers[1].APIBackend = "messages"
	providers.Providers[1].APIKey = "updated-key"
	providers.Providers[1].ModelIDs = []string{"shared-model", "added-model"}
	if err := reconcileProviders(t.Context(), database, vault, providers, state, Options{}); err == nil {
		t.Fatal("configuration changes applied without --update")
	}
	for range 2 {
		if err := reconcileProviders(t.Context(), database, vault, providers, state, Options{Update: true}); err != nil {
			t.Fatal(err)
		}
	}
	for i, previous := range original {
		current, err := database.GetLLMProvider(t.Context(), state.Organization.UUID, previous.WorkspaceUUID, previous.ExternalID)
		if err != nil || current.UUID != previous.UUID || current.Slug != previous.Slug {
			t.Fatalf("provider identity changed: %+v, %v", current, err)
		}
		qualified := llmproviders.QualifiedModelID(previous.Slug, "shared-model")
		upstream, err := llmproviders.Resolve(t.Context(), database, vault, state.Organization.UUID, previous.WorkspaceUUID, qualified)
		if err != nil || upstream.QualifiedModelID != qualified || upstream.ModelID != "shared-model" {
			t.Fatalf("model reference changed: %+v, %v", upstream, err)
		}
		if previous.Name == "second" && (upstream.APIKey != "updated-key" || upstream.BaseURL != "https://updated.example" || upstream.APIBackend != "messages" || len(current.Models) != 2) {
			t.Fatalf("update not applied to provider %d", i)
		}
	}
	duplicate := providers.Providers[0]
	duplicate.Slug = "duplicate-name"
	if _, err := createProvider(t.Context(), database, vault, state.Organization.UUID, state.Workspaces[0], duplicate); err != nil {
		t.Fatal(err)
	}
	if err := reconcileProviders(t.Context(), database, vault, providers, state, Options{}); err != nil {
		t.Fatalf("registered identity must survive duplicate display names: %v", err)
	}
	unregistered := *state
	unregistered.Providers = nil
	if err := reconcileProviders(t.Context(), database, vault, providers, &unregistered, Options{}); err == nil {
		t.Fatal("ambiguous name recovery was accepted")
	}
	registered := state.Providers[0]
	if err := database.DeleteLLMProvider(t.Context(), state.Organization.UUID, state.Workspaces[0].UUID, registered.ExternalID); err != nil {
		t.Fatal(err)
	}
	if err := reconcileProviders(t.Context(), database, vault, providers, state, Options{Update: true}); err == nil {
		t.Fatal("deleted registered provider was replaced by a same-name provider")
	}
	if _, err := database.GetLLMProvider(t.Context(), state.Organization.UUID, state.Workspaces[0].UUID, registered.ExternalID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("deleted provider was recreated: %v", err)
	}
}
