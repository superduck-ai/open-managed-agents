//go:build integration

package webtools

import (
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"os"
	"strings"
	"testing"
)

func TestLiveWebTools(t *testing.T) {
	apiKey := os.Getenv("OMA_WEBTOOLS_LIVE_API_KEY")
	if apiKey == "" {
		t.Skip("set OMA_WEBTOOLS_LIVE_API_KEY for real DeepSeek and HTTP acceptance")
	}
	client, err := Load(config.WebToolsConfig{SearchProvider: "deepseek", Providers: map[string]config.WebToolProviderConfig{"deepseek": {APIKey: apiKey}}})
	if err != nil {
		t.Fatal("could not load live search credentials")
	}
	policy := network(t, `{}`)
	found, err := client.Search(t.Context(), SearchRequest{Query: "DeepSeek official API documentation"}, policy)
	if err != nil || len(found.Sources) == 0 {
		t.Fatalf("live search failed: %v", err)
	}
	page, err := client.Fetch(t.Context(), FetchRequest{URL: "https://example.com"}, policy)
	if err != nil || page.StatusCode != 200 || page.Title != "Example Domain" || !strings.Contains(page.Content, "documentation examples") {
		t.Fatalf("live fetch failed: status=%d title=%q bytes=%d err=%v", page.StatusCode, page.Title, len(page.Content), err)
	}
	policy = network(t, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com"]}}`)
	if _, err := client.Fetch(t.Context(), FetchRequest{URL: "https://example.com"}, policy); err != nil {
		t.Fatalf("live limited fetch failed: %v", err)
	}
	if _, err := client.Fetch(t.Context(), FetchRequest{URL: "https://www.iana.org"}, policy); err == nil {
		t.Fatal("live limited fetch bypassed allowlist")
	}
}
