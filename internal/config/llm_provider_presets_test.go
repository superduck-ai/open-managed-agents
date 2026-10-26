package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
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

func TestLLMProviderPresetWriteConflictAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, defaultLLMProviderPresets, 0640); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadLLMProviderPresetCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	models := []LLMPresetModel{{ID: "second", Name: "Second", ContextWindow: new(128000)}, {ID: "first"}}
	if _, err := SaveLLMProviderPresetModels(path, "deepseek", "stale", models); !errors.Is(err, ErrPresetConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	if _, err := SaveLLMProviderPresetModels(path, "missing", catalog.Revision, models); !errors.Is(err, ErrPresetNotFound) {
		t.Fatalf("expected missing: %v", err)
	}
	if _, err := SaveLLMProviderPresetModels(path, "deepseek", catalog.Revision, []LLMPresetModel{{ID: ""}}); err == nil {
		t.Fatal("expected invalid model")
	}
	unchanged, _ := ReadLLMProviderPresetCatalog(path)
	if unchanged.Revision != catalog.Revision {
		t.Fatal("failed save changed file")
	}
	saved, err := SaveLLMProviderPresetModels(path, "deepseek", catalog.Revision, models)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := ReadLLMProviderPresetCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Revision != saved.Revision || restarted.Providers[0].Models[0].ID != "second" || restarted.Providers[0].Models[0].Name != "Second" || *restarted.Providers[0].Models[0].ContextWindow != 128000 {
		t.Fatalf("lost model metadata/order: %+v", restarted)
	}
	if len(restarted.Providers[5].Models) != 0 {
		t.Fatal("changed other supplier")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Fatal("changed permissions")
	}
	empty, err := SaveLLMProviderPresetModels(path, "deepseek", saved.Revision, []LLMPresetModel{})
	if err != nil || len(empty.Providers[0].Models) != 0 {
		t.Fatalf("empty models rejected: %v", err)
	}
}

func TestLLMProviderPresetConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	if err := os.WriteFile(path, defaultLLMProviderPresets, 0600); err != nil {
		t.Fatal(err)
	}
	catalog, _ := ReadLLMProviderPresetCatalog(path)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"one", "two"} {
		wg.Go(func() {
			_, err := SaveLLMProviderPresetModels(path, "deepseek", catalog.Revision, []LLMPresetModel{{ID: id}})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrPresetConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes %d conflicts %d", successes, conflicts)
	}
}

func TestLLMProviderPresetEmbeddedAndEmpty(t *testing.T) {
	if _, err := SaveLLMProviderPresetModels("", "deepseek", "", nil); !errors.Is(err, ErrPresetReadOnly) {
		t.Fatal(err)
	}
	catalog, err := ReadLLMProviderPresetCatalog("")
	if err != nil || catalog.Writable || len(catalog.Providers) != 7 {
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
