package webtools

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.deepseek.com" || r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Error("invalid provider boundary")
		}
		clone := r.Clone(r.Context())
		clone.URL = r.URL.Clone()
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return http.DefaultTransport.RoundTrip(clone)
	})
	return NewClient("private-test-key", client)
}

func network(t *testing.T, raw string) networkpolicy.Config {
	t.Helper()
	p, err := networkpolicy.ParseConfig([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWebToolsRejectUnsafeAndInvalidInput(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached provider") })
	p := network(t, `{"type":"cloud"}`)
	for _, url := range []string{"http://127.0.0.1", "http://127.1", "http://0x7f000001", "http://[::1]", "http://169.254.169.254", "file:///etc/passwd", "https://user:pass@example.com", "http://host.docker.internal", "http://example.com:8080"} {
		if _, err := c.Fetch(t.Context(), FetchRequest{URL: url}, p); err == nil {
			t.Errorf("accepted %s", url)
		}
	}
	for _, input := range []SearchRequest{{Query: " "}, {Query: strings.Repeat("中", 129)}, {Query: "test", Count: new(0)}, {Query: "test", Count: new(51)}, {Query: "test", OriginalQuestion: new("")}} {
		if _, err := c.Search(t.Context(), input, p); err == nil {
			t.Error("accepted invalid search")
		}
	}
	if _, err := c.Fetch(t.Context(), FetchRequest{URL: "https://example.com"}, network(t, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com"]}}`)); err == nil {
		t.Error("limited fetch bypassed redirect policy")
	}
}

func TestWebToolsProviderFailureDoesNotLeakSecrets(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "secret private-test-key provider diagnostic")
			})
			_, err := c.Search(t.Context(), SearchRequest{Query: "test"}, network(t, `{}`))
			app, ok := errors.AsType[*apperr.Error](err)
			if !ok || strings.Contains(app.PublicMessage, "private-test-key") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
			want := int32(1)
			if retryable(status) {
				want = 2
			}
			if calls.Load() != want {
				t.Errorf("calls=%d want=%d", calls.Load(), want)
			}
		})
	}
	for _, raw := range []string{`broken`, `{}`, `{"results":null}`, strings.Repeat("x", (2<<20)+1)} {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, raw) })
		if _, err := c.Search(t.Context(), SearchRequest{Query: "test"}, network(t, `{}`)); err == nil {
			t.Error("accepted malformed response")
		}
	}
}

func TestWebToolsCancellationStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var count atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { count.Add(1); cancel(); w.WriteHeader(503) })
	started := time.Now()
	if _, err := c.Search(ctx, SearchRequest{Query: "test"}, network(t, `{}`)); err == nil {
		t.Fatal("cancelled request succeeded")
	}
	if count.Load() != 1 || time.Since(started) > time.Second {
		t.Fatal("cancellation did not stop backoff")
	}
}

func TestWebSearchPreservesDocumentedFields(t *testing.T) {
	expected := SearchRequest{Query: "DeepSeek site:example.com", Count: new(5), OriginalQuestion: new("original question"), Localization: "8.8.8.8", NeedContent: true}
	var want SearchResponse
	raw := `{"results":[{"rerank_score":2.4,"title":"Example","url":"https://example.com/article","snippet":"snippet","summary":"summary","content":"Markdown body","site_name":"Example","site_icon":"https://example.com/favicon.ico","date":"2026-10-20T00:00:00Z","authority":3}]}`
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var input SearchRequest
		if json.Unmarshal(body, &input) != nil {
			t.Error("invalid provider JSON")
		}
		actual, _ := json.Marshal(input)
		expectedJSON, _ := json.Marshal(expected)
		if string(actual) != string(expectedJSON) {
			t.Errorf("input=%s want=%s", actual, expectedJSON)
		}
		_, _ = io.WriteString(w, raw)
	})
	output, err := c.Search(t.Context(), expected, network(t, `{}`))
	actual, _ := json.Marshal(output)
	wantJSON, _ := json.Marshal(want)
	if err != nil || string(actual) != string(wantJSON) {
		t.Fatalf("response=%s want=%s error=%v", actual, wantJSON, err)
	}
}

func TestWebSearchAcceptsEmptyResults(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"results":[]}`)
	})
	output, err := c.Search(t.Context(), SearchRequest{Query: "no matches"}, network(t, `{}`))
	if err != nil || output.Results == nil || len(output.Results) != 0 {
		t.Fatalf("output=%+v error=%v", output, err)
	}
}

func TestWebSearchEnforcesEnvironmentHosts(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var input SearchRequest
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &input)
		if input.Query != "test site:example.com" {
			t.Errorf("query=%s", input.Query)
		}
		_, _ = io.WriteString(w, `{"results":[{"url":"https://example.com/article"},{"url":"https://notexample.com"}]}`)
	})
	output, err := c.Search(t.Context(), SearchRequest{Query: "test"}, network(t, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["example.com"]}}`))
	if err != nil || len(output.Results) != 1 {
		t.Fatalf("output=%+v error=%v", output, err)
	}
}

func TestWebFetchUsesURLOnlyAndReturnsTitleAndMarkdown(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != `{"url":"https://example.com/article"}` {
			t.Errorf("fetch body=%s", raw)
		}
		_, _ = io.WriteString(w, `{"title":"Example","content":"# Example\n\nMarkdown body"}`)
	})
	output, err := c.Fetch(t.Context(), FetchRequest{URL: "https://example.com/article"}, network(t, `{}`))
	if err != nil || output.Title != "Example" || output.Content != "# Example\n\nMarkdown body" {
		t.Fatalf("output=%+v error=%v", output, err)
	}
}
