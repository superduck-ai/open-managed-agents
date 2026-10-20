package codesessions

import (
	"errors"
	"maps"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestModelBackendsRejectUnsupportedBackend(t *testing.T) {
	_, err := modelBackendsForProviders([]db.LLMProvider{{Slug: "oma", APIBackend: "responses", ModelIDs: []string{"kimi-k2.5"}}})
	if !errors.Is(err, llmproviders.ErrBackendMismatch) {
		t.Fatalf("err = %v, want ErrBackendMismatch", err)
	}
}

func TestModelBackendsKeyQualifiedAndUnambiguousLegacyIDs(t *testing.T) {
	backends, err := modelBackendsForProviders([]db.LLMProvider{
		{Slug: "oma", APIBackend: llmproviders.ChatCompletions, ModelIDs: []string{"kimi-k2.5", "deepseek-ai/deepseek-v4.1-flash"}},
		{Slug: "default", APIBackend: llmproviders.Messages, ModelIDs: []string{"kimi-k2.5", "glm-5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"oma/kimi-k2.5":                       llmproviders.ChatCompletions,
		"oma/deepseek-ai/deepseek-v4.1-flash": llmproviders.ChatCompletions,
		"deepseek-ai/deepseek-v4.1-flash":     llmproviders.ChatCompletions,
		"default/kimi-k2.5":                   llmproviders.Messages,
		"default/glm-5":                       llmproviders.Messages,
		"glm-5":                               llmproviders.Messages,
	}
	if !maps.Equal(backends, want) {
		t.Fatalf("backends = %#v, want %#v", backends, want)
	}
}
