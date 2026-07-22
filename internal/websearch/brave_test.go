package websearch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestBraveClientFailures(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		client := NewBraveClient(BraveClientConfig{}, nil)
		_, err := client.Search(context.Background(), SearchRequest{Query: "query"})
		if err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid page token", func(t *testing.T) {
		client := NewBraveClient(BraveClientConfig{Endpoint: "http://example.test", APIKey: "key", Timeout: time.Second}, nil)
		_, err := client.Search(context.Background(), SearchRequest{Query: "query", Options: SearchOptions{PageToken: "10"}})
		if err == nil || !strings.Contains(err.Error(), "page token") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("provider error does not expose response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "secret provider detail")
		}))
		defer server.Close()
		client := NewBraveClient(BraveClientConfig{Endpoint: server.URL, APIKey: "key", Timeout: time.Second}, nil)
		_, err := client.Search(context.Background(), SearchRequest{Query: "query"})
		if err == nil || strings.Contains(err.Error(), "secret provider detail") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "{")
		}))
		defer server.Close()
		client := NewBraveClient(BraveClientConfig{Endpoint: server.URL, APIKey: "key", Timeout: time.Second}, nil)
		_, err := client.Search(context.Background(), SearchRequest{Query: "query"})
		if err == nil || !strings.Contains(err.Error(), "decode brave") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("date options override defaults", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("freshness"); got != "2025-01-01to2025-01-31" {
				t.Errorf("freshness = %q", got)
			}
			_, _ = io.WriteString(w, "{\"query\":{\"more_results_available\":false},\"web\":{\"results\":[]}}")
		}))
		defer server.Close()
		client := NewBraveClient(BraveClientConfig{Endpoint: server.URL, APIKey: "key", Timeout: time.Second, Options: SearchOptions{Freshness: "pw"}}, nil)
		_, err := client.Search(context.Background(), SearchRequest{
			Query: "query",
			Options: SearchOptions{
				StartPublishedAt: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
				EndPublishedAt:   time.Date(2025, time.January, 31, 0, 0, 0, 0, time.UTC),
			},
		})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
	})
}

func TestBraveClientSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("X-Subscription-Token") != "brave-key" {
			t.Fatalf("request = %s %s, token = %q", r.Method, r.URL, r.Header.Get("X-Subscription-Token"))
		}
		query := r.URL.Query()
		for key, want := range map[string]string{
			"q": "golang release", "count": "20", "country": "US", "search_lang": "en",
			"ui_lang": "en-US", "freshness": "pw", "safesearch": "strict", "spellcheck": "true",
			"result_filter": "web", "extra_snippets": "true", "units": "metric", "offset": "2",
		} {
			if query.Get(key) != want {
				t.Errorf("query[%q] = %q, want %q", key, query.Get(key), want)
			}
		}
		if got := query["goggles"]; len(got) != 2 || got[0] != "goggle-a" || got[1] != "goggle-b" {
			t.Errorf("goggles = %#v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"id\":\"request-1\",\"query\":{\"more_results_available\":true},\"web\":{\"results\":[{\"id\":\"result-1\",\"title\":\"Go\",\"url\":\"https://go.dev\",\"description\":\"release notes\",\"page_age\":\"2d\",\"extra_snippets\":[\"snippet 2\"],\"profile\":{\"long_name\":\"Go Project\"},\"meta_url\":{\"favicon\":\"https://go.dev/favicon.ico\"}}]}}")
	}))
	defer server.Close()
	spellcheck := true
	client := NewBraveClient(BraveClientConfig{Endpoint: server.URL, APIKey: "brave-key", Timeout: time.Second, Options: SearchOptions{
		MaxResults: 30, Country: "US", SearchLanguage: "en", UILanguage: "en-US", Freshness: "pw",
		SafeSearch: "strict", Spellcheck: &spellcheck, ResultFilter: "web", Goggles: []string{"goggle-a", "goggle-b"},
		ExtraSnippets: true, Units: "metric",
	}}, nil)
	response, err := client.Search(context.Background(), SearchRequest{Query: " golang release ", Options: SearchOptions{PageToken: "2"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !response.HasMore || response.NextPageToken != "3" || response.RequestID != "request-1" || len(response.Results) != 1 {
		t.Fatalf("response = %#v", response)
	}
	result := response.Results[0]
	if result.ID != "result-1" || result.Title != "Go" || result.Snippet != "release notes" ||
		result.PageAge != "2d" || result.Author != "Go Project" || result.Favicon == "" || len(result.ExtraSnippets) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestNewProviderBrave(t *testing.T) {
	provider := NewProvider(config.WebSearchConfig{Provider: "brave", APIKey: "key"}, nil)
	if _, ok := provider.(*BraveClient); !ok {
		t.Fatalf("provider = %T, want *BraveClient", provider)
	}
}
