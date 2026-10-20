package webtools

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
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
		if r.URL.Host == "api.deepseek.com" && r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Error("missing provider credential")
		}
		if r.URL.Host != "api.deepseek.com" && r.Header.Get("Authorization") != "" {
			t.Error("credential leaked to fetched page")
		}
		clone := r.Clone(r.Context())
		clone.URL = r.URL.Clone()
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(server.URL, "http://")
		response, err := http.DefaultTransport.RoundTrip(clone)
		if response != nil {
			response.Request = r
		}
		return response, err
	})
	return NewClient(NewDeepSeekSearch("private-test-key", client), client.Transport)
}

func network(t *testing.T, raw string) Policy {
	t.Helper()
	p, err := networkpolicy.ParseConfig([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return Policy{Environment: p}
}

func TestWebToolsRejectUnsafeAndInvalidInput(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached network") })
	p := network(t, `{}`)
	for _, u := range []string{"http://127.0.0.1", "http://127.1", "http://0x7f000001", "http://[::1]", "http://169.254.169.254", "file:///etc/passwd", "https://user:pass@example.com", "http://host.docker.internal", "http://example.com:8080"} {
		if _, err := c.Fetch(t.Context(), FetchRequest{URL: u}, p); err == nil {
			t.Errorf("accepted %s", u)
		}
	}
	for _, q := range []string{" ", strings.Repeat("中", 129)} {
		if _, err := c.Search(t.Context(), SearchRequest{Query: q}, p); err == nil {
			t.Error("accepted invalid search")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if conn, err := publicDial(ctx, "tcp", "127.0.0.1:80", net.DefaultResolver); err == nil {
		conn.Close()
		t.Fatal("private IP accepted")
	}
}

func TestWebToolsProviderFailuresAndCancellation(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				io.WriteString(w, "private-test-key diagnostic")
			})
			_, err := c.Search(t.Context(), SearchRequest{Query: "test"}, network(t, `{}`))
			app, ok := errors.AsType[*apperr.Error](err)
			if !ok || strings.Contains(app.PublicMessage, "private-test-key") {
				t.Fatalf("unsafe error %v", err)
			}
			want := int32(1)
			if retryable(status) {
				want = 2
			}
			if calls.Load() != want {
				t.Fatalf("calls=%d", calls.Load())
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	var count atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { count.Add(1); cancel(); w.WriteHeader(503) })
	if _, err := c.Search(ctx, SearchRequest{Query: "test"}, network(t, `{}`)); err == nil || count.Load() != 1 {
		t.Fatal("cancellation did not stop retries")
	}
	for _, raw := range []string{`broken`, `{}`, `{"results":null}`, strings.Repeat("x", (2<<20)+1)} {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, raw) })
		if _, err := c.Search(t.Context(), SearchRequest{Query: "test"}, network(t, `{}`)); err == nil {
			t.Error("accepted invalid response")
		}
	}
}

func TestWebSearchFiltersSourcesWithoutLeakingProviderFields(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Query       string `json:"query"`
			Count       int    `json:"count"`
			NeedContent bool   `json:"need_content"`
		}
		if json.UnmarshalRead(r.Body, &input) != nil || input.Query != "test" || input.NeedContent || input.Count != 10 {
			t.Error("invalid search adapter request")
		}
		io.WriteString(w, `{"results":[{"url":"https://docs.example.com/guide","title":"Docs","snippet":"body","summary":"discard","authority":3},{"url":"https://example.com"},{"url":"https://evil.example.net"}]}`)
	})
	p := network(t, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["*.example.com"]}}`)
	p.Tools = []ToolConfig{{AllowedDomains: []string{"example.com"}}, {BlockedDomains: []string{"evil.example.com"}}}
	out, err := c.Search(t.Context(), SearchRequest{Query: "test"}, p)
	if err != nil || len(out.Sources) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "summary") || strings.Contains(string(raw), "authority") {
		t.Fatal("provider contract leaked")
	}
}

func TestWebFetchLimitedRedirectsBudgetAndNoPaidFetch(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/escape":
			http.Redirect(w, r, "https://blocked.example.net/body", 302)
		case "/redirect":
			http.Redirect(w, r, "https://docs.example.com/body", 302)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, "<title>Docs</title><script>secret()</script><h1>Hello</h1><p>"+strings.Repeat("中", 30)+"</p>")
		}
	})
	p := network(t, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["docs.example.com"]}}`)
	p.Tools = []ToolConfig{{AllowedDomains: []string{"example.com"}, MaxContentTokens: new(20)}}
	_, err := c.Fetch(t.Context(), FetchRequest{URL: "https://docs.example.com/escape"}, p)
	app, ok := errors.AsType[*apperr.Error](err)
	if !ok || app.PublicMessage != "url_not_allowed" {
		t.Fatalf("redirect escaped: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("denied redirect was sent")
	}
	out, err := c.Fetch(t.Context(), FetchRequest{URL: "https://docs.example.com/redirect"}, p)
	if err != nil || out.Title != "Docs" || !out.Truncated || len(out.Content) > 20 || !strings.Contains(out.FullContent, "中") || strings.Contains(out.FullContent, "secret") {
		t.Fatalf("%+v %v", out, err)
	}
	if out.URL != "https://docs.example.com/body" || !strings.HasPrefix(out.FullContent, "# Hello") {
		t.Fatalf("bad fetched content %+v", out)
	}
}

func TestFetchSizeBinaryTimeoutAndMissingSearchKey(t *testing.T) {
	if _, err := (*Client)(nil).Search(t.Context(), SearchRequest{Query: "test"}, network(t, `{}`)); err == nil {
		t.Fatal("search without key succeeded")
	}
	for _, mode := range []string{"declared", "stream", "binary", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				switch mode {
				case "declared":
					w.Header().Set("Content-Length", "5000001")
				case "stream":
					io.WriteString(w, strings.Repeat("x", maxFetchBytes+1))
				case "binary":
					w.Header().Set("Content-Type", "image/png")
				case "cancel":
					<-r.Context().Done()
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			if _, err := c.Fetch(ctx, FetchRequest{URL: "https://example.com"}, network(t, `{}`)); err == nil {
				t.Fatal("invalid fetch accepted")
			}
		})
	}
}

type searchProviderFunc func(context.Context, SearchRequest) (SearchResponse, error)

func (f searchProviderFunc) Search(ctx context.Context, input SearchRequest) (SearchResponse, error) {
	return f(ctx, input)
}

func TestReplacementSearchProviderPreservesPolicyAndCancellation(t *testing.T) {
	started := make(chan struct{})
	provider := searchProviderFunc(func(ctx context.Context, input SearchRequest) (SearchResponse, error) {
		if input.Query == "cancel" {
			close(started)
			<-ctx.Done()
			return SearchResponse{}, ctx.Err()
		}
		if input.Query != "replacement" {
			t.Errorf("provider received unnormalized query %q", input.Query)
		}
		return SearchResponse{Sources: []Source{
			{Title: "Allowed", URL: "https://docs.example.com"},
			{Title: "Blocked", URL: "https://example.net"},
			{Title: "Private", URL: "http://127.0.0.1"},
		}}, nil
	})
	client := NewClient(provider, nil)
	policy := network(t, `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["docs.example.com"]}}`)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := client.Search(ctx, SearchRequest{Query: "cancel"}, policy)
		finished <- err
	}()
	<-started
	result, err := client.Search(t.Context(), SearchRequest{Query: "  replacement  "}, policy)
	if err != nil || len(result.Sources) != 1 || result.Sources[0].Title != "Allowed" {
		t.Fatalf("replacement provider bypassed policy: %+v %v", result, err)
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("provider cancellation lost: %v", err)
	}
}
