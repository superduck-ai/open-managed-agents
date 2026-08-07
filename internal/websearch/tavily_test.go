package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTavilyClientFailures(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		client := NewTavilyClient("", "", time.Second, nil)
		_, err := client.Search(context.Background(), SearchRequest{Query: "query"})
		if err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Fatalf("error = %v, want not configured", err)
		}
	})
	t.Run("empty query", func(t *testing.T) {
		client := NewTavilyClient("", "test-key", time.Second, nil)
		_, err := client.Search(context.Background(), SearchRequest{Query: "  "})
		if err == nil || !strings.Contains(err.Error(), "query is required") {
			t.Fatalf("error = %v, want query required", err)
		}
	})
	t.Run("provider error does not expose response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "secret provider detail")
		}))
		defer server.Close()
		client := NewTavilyClient(server.URL, "test-key", time.Second, nil)
		_, err := client.Search(context.Background(), SearchRequest{Query: "query"})
		if err == nil || strings.Contains(err.Error(), "secret provider detail") {
			t.Fatalf("error = %v, must be generic", err)
		}
	})
}

func TestTavilyClientSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["query"] != "go release" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"results\":[{\"title\":\"Go\",\"url\":\"https://go.dev\",\"content\":\"release notes\"}]}")
	}))
	defer server.Close()
	client := NewTavilyClient(server.URL, "test-key", time.Second, nil)
	response, err := client.Search(context.Background(), SearchRequest{
		Query:   " go release ",
		Options: SearchOptions{MaxResults: 2},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://go.dev" || response.Results[0].Snippet != "release notes" {
		t.Fatalf("results = %#v", response.Results)
	}
}
