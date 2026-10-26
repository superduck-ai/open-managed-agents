package llmproviders

import (
	"errors"
	"maps"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestModelBackendsRejectUnsupportedBackend(t *testing.T) {
	_, err := NewCatalog([]db.LLMProvider{{Slug: "oma", APIBackend: "responses", Models: db.LLMModels{{ID: "kimi-k2.5"}}}}).RuntimeModels()
	if !errors.Is(err, ErrBackendMismatch) {
		t.Fatalf("err = %v, want ErrBackendMismatch", err)
	}
}

func TestModelBackendsKeyQualifiedAndUnambiguousLegacyIDs(t *testing.T) {
	backends, err := NewCatalog([]db.LLMProvider{
		{Slug: "oma", APIBackend: ChatCompletions, Models: db.LLMModels{{ID: "kimi-k2.5"}, {ID: "deepseek-ai/deepseek-v4.1-flash"}}},
		{Slug: "default", APIBackend: Messages, Models: db.LLMModels{{ID: "kimi-k2.5"}, {ID: "glm-5"}}},
	}).RuntimeModels()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"oma/kimi-k2.5":                       ChatCompletions,
		"oma/deepseek-ai/deepseek-v4.1-flash": ChatCompletions,
		"deepseek-ai/deepseek-v4.1-flash":     ChatCompletions,
		"default/kimi-k2.5":                   Messages,
		"default/glm-5":                       Messages,
		"glm-5":                               Messages,
	}
	if !maps.Equal(runtimeBackends(backends), want) {
		t.Fatalf("backends = %#v, want %#v", backends, want)
	}
}

func runtimeBackends(models map[string]RuntimeModel) map[string]string {
	result := make(map[string]string)
	for id, model := range models {
		result[id] = model.APIBackend
	}
	return result
}
