package websearch

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestNewProviderRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config config.WebSearchConfig
		want   string
	}{
		{
			name:   "unsupported provider",
			config: config.WebSearchConfig{Provider: "exa"},
			want:   "unsupported",
		},
		{
			name:   "missing provider configuration",
			config: config.WebSearchConfig{Provider: "tavily"},
			want:   "no configuration",
		},
		{
			name: "unknown Brave option",
			config: config.WebSearchConfig{
				Provider: "brave",
				Providers: map[string]config.WebSearchProviderConfig{
					"brave": {
						APIKey:  "key",
						Options: map[string]json.RawMessage{"not_a_brave_option": json.RawMessage(`true`)},
					},
				},
			},
			want: "decode provider options",
		},
		{
			name: "invalid Brave option value",
			config: config.WebSearchConfig{
				Provider: "brave",
				Providers: map[string]config.WebSearchProviderConfig{
					"brave": {
						APIKey:  "key",
						Options: map[string]json.RawMessage{"safe_search": json.RawMessage(`"unsafe"`)},
					},
				},
			},
			want: "safe_search",
		},
		{
			name: "invalid Brave date",
			config: config.WebSearchConfig{
				Provider: "brave",
				Providers: map[string]config.WebSearchProviderConfig{
					"brave": {
						APIKey:  "key",
						Options: map[string]json.RawMessage{"start_published_at": json.RawMessage(`"yesterday"`)},
					},
				},
			},
			want: "start_published_at",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewProvider(test.config, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewProvider() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBuiltInProviderFactories(t *testing.T) {
	if len(builtInProviderFactories) != 2 {
		t.Fatalf("built-in factory count = %d, want 2", len(builtInProviderFactories))
	}
	for _, name := range []string{"brave", "tavily"} {
		if _, ok := builtInProviderFactories[name]; !ok {
			t.Fatalf("built-in factory %q is missing", name)
		}
	}
}

func TestNewProviderUsesBuiltInProviderFactory(t *testing.T) {
	provider, err := NewProvider(config.WebSearchConfig{
		Provider: "TAVILY",
		Providers: map[string]config.WebSearchProviderConfig{
			"tavily": {APIKey: "key"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if _, ok := provider.(*TavilyClient); !ok {
		t.Fatalf("provider = %T, want *TavilyClient", provider)
	}
}
