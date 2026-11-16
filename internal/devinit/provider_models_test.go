package devinit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const detailedProvider = `version: 1
providers:
  - name: deepseek
    api_backend: messages
    base_url: https://api.deepseek.com/anthropic
    api_key: fixture-key
    models:
      - id: deepseek-flash
        name: DeepSeek
        context_window: 262144
        max_output_tokens: 32768
        input_modalities: [text, image]
        image_input:
          max_images: 8
          max_long_edge: 2048
`

func TestProviderModelsRejectInvalidMetadata(t *testing.T) {
	for name, replacement := range map[string][2]string{
		"both":               {"    models:", "    model_ids: [other]\n    models:"},
		"unknown":            {"max_images: 8", "mystery: 8"},
		"null":               {"max_images: 8", "max_images: null"},
		"capacity":           {"context_window: 262144", "context_window: 0"},
		"modality":           {"[text, image]", "[text, audio]"},
		"duplicate modality": {"[text, image]", "[text, text]"},
		"empty modality":     {"[text, image]", "[]"},
		"image limit":        {"max_images: 8", "max_images: -1"},
		"duplicate model":    {"    models:", "    models:\n      - id: deepseek-flash"},
		"empty id":           {"id: deepseek-flash", "id: ''"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadModelProviders(writeProvidersFile(t, strings.Replace(detailedProvider, replacement[0], replacement[1], 1))); err == nil {
				t.Fatal("invalid model metadata accepted")
			}
		})
	}
}

func TestProviderModelsLoadAndPreserveMetadata(t *testing.T) {
	spec, err := LoadModelProviders(writeProvidersFile(t, strings.Replace(detailedProvider, "id: deepseek-flash", "id: ' deepseek-flash '", 1)))
	if err != nil {
		t.Fatal(err)
	}
	p := spec.Providers[0]
	if !reflect.DeepEqual(p.ModelIDs, []string{"deepseek-flash"}) || len(p.Models) != 1 {
		t.Fatal("model membership was lost")
	}
	m := p.Models[0]
	if *m.ContextWindow != 262144 || *m.MaxOutputTokens != 32768 || len(m.InputModalities) != 2 || m.ImageInput.MaxImages != 8 || m.ImageInput.MaxLongEdge != 2048 {
		t.Fatal("model metadata was lost")
	}
	existing := db.LLMModels{{ID: m.ID, Name: "previous", InputModalities: []string{"text"}}, {ID: "removed"}}
	if got := configuredProviderModels(existing, p); !reflect.DeepEqual(got, p.Models) {
		t.Fatal("explicit metadata did not replace configuration")
	}
	legacy := configuredProviderModels(existing, ProviderSpec{ModelIDs: []string{m.ID, "new"}})
	if legacy[0].Name != "previous" || len(legacy[0].InputModalities) != 1 || legacy[1].ID != "new" {
		t.Fatal("legacy ids discarded existing metadata")
	}
}
