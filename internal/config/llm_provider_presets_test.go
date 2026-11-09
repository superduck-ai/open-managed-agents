package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLLMProviderPresetInvalidFiles(t *testing.T) {
	for _, data := range []string{
		`{"version":2,"providers":[]}`,
		`{"version":1,"providers":null}`,
		`{"version":1,"providers":[],"api_key":"secret"}`,
		`{"version":1,"providers":[{"id":"a","name":"A","api_backend":"chat_completions","base_url":"https://example.com","models":[{"id":"m"},{"id":"m"}]}]}`,
		`{"version":1,"providers":[{"id":"a","name":"A","api_backend":"messages","base_url":"https://secret@example.com","models":[]}]}`,
	} {
		t.Run(data, func(t *testing.T) {
			if _, err := decodeLLMProviderPresets([]byte(data)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLLMProviderPresetEmbeddedAndEmpty(t *testing.T) {
	catalog, err := ReadLLMProviderPresetCatalog("")
	if err != nil || len(catalog.Providers) != 7 {
		t.Fatalf("embedded: %+v %v", catalog, err)
	}
	providers, err := decodeLLMProviderPresets([]byte(`{"version":1,"providers":[]}`))
	if err != nil || len(providers) != 0 {
		t.Fatalf("empty: %v", err)
	}
}

func TestLLMProviderPresetOrder(t *testing.T) {
	providers, err := decodeLLMProviderPresets([]byte(`{"version":1,"providers":[
 {"id":"kimi","name":"Kimi","order":20,"api_backend":"messages","base_url":"https://api.kimi.com/coding","models":[]},
 {"id":"gateway","name":"Internal Gateway","order":0,"api_backend":"messages","base_url":"https://gateway.example.com","models":[]},
 {"id":"glm","name":"GLM","order":20,"api_backend":"messages","base_url":"https://open.bigmodel.cn/api/anthropic","models":[]}
 ]}`))
	if err != nil {
		t.Fatal(err)
	}
	if providers[0].ID != "gateway" || providers[1].ID != "kimi" || providers[2].ID != "glm" {
		t.Fatalf("unexpected supplier order: %+v", providers)
	}
	providers[0].Order = -1
	if err := normalizeLLMProviderPreset(&providers[0]); err == nil {
		t.Fatal("negative order accepted")
	}
}

func TestLLMProviderPresetConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, defaultLLMProviderPresets, 0400); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadLLMProviderPresetCatalog(path)
	if err != nil || len(catalog.Providers) != 7 {
		t.Fatalf("configured catalog: %+v %v", catalog, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(defaultLLMProviderPresets) {
		t.Fatalf("catalog changed: %v", err)
	}
}
