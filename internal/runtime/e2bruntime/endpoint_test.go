package e2bruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestPublishedServiceEndpointRejectsInvalidMapping(t *testing.T) {
	for _, payload := range []string{
		`{}`, `{`,
		`{"containerPort":3002,"hostPort":32768,"url":"http://localhost:32768"}`,
		`{"containerPort":3001,"hostPort":0,"url":"http://localhost:0"}`,
		`{"containerPort":3001,"hostPort":32768,"url":"http://localhost:32769"}`,
		`{"containerPort":3001,"hostPort":32768,"url":"http://user:secret@localhost:32768"}`,
		`{"containerPort":3001,"hostPort":32768,"url":"http://localhost:32768/?token=x"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
		provider := NewProvider(config.E2BConfig{APIURL: server.URL, LocalPortLookup: true})
		if _, err := provider.ServiceEndpoint(t.Context(), "sbx_test", 3001, "/mcp"); err == nil {
			t.Fatalf("accepted invalid mapping %s", payload)
		}
		server.Close()
	}
}

func TestPublishedServiceEndpointDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	provider := NewProvider(config.E2BConfig{APIURL: server.URL, APIKey: "private-test-key", LocalPortLookup: true})
	if _, err := provider.ServiceEndpoint(t.Context(), "sbx_test", 3001, "/mcp"); err == nil || forwarded {
		t.Fatalf("redirect followed=%t error=%v", forwarded, err)
	}
}

func TestPublishedServiceEndpointUsesMappedPortAndHostOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sandboxes/sbx_test/ports/3001" || r.Header.Get("X-API-Key") != "local" {
			t.Error("unexpected port lookup request")
		}
		_, _ = w.Write([]byte(`{"containerPort":3001,"hostPort":32768,"url":"http://198.18.0.1:32768"}`))
	}))
	defer server.Close()
	for _, tc := range []struct{ host, want string }{
		{"", "http://198.18.0.1:32768/mcp"},
		{"127.0.0.1", "http://127.0.0.1:32768/mcp"},
		{"::1", "http://[::1]:32768/mcp"},
	} {
		provider := NewProvider(config.E2BConfig{APIURL: server.URL, APIKey: "local", LocalPortLookup: true, LocalServiceHost: tc.host})
		got, err := provider.ServiceEndpoint(context.Background(), "sbx_test", 3001, "/mcp")
		if err != nil || got != tc.want {
			t.Fatalf("endpoint=%s want=%s error=%v", got, tc.want, err)
		}
	}
}
