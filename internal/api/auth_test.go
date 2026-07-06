package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIEntrypointRouterDispatchesByHost(t *testing.T) {
	handler := apiEntrypointRouter{
		service: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("service"))
		}),
		platform: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("platform"))
		}),
	}
	tests := []struct {
		name   string
		host   string
		apiKey string
		want   string
	}{
		{name: "platform host", host: "platform.claude.com", want: "platform"},
		{name: "platform host with port", host: "platform.claude.com:443", want: "platform"},
		{name: "platform host with api key", host: "platform.claude.com", apiKey: "sk-ant-local-test", want: "platform"},
		{name: "platform subdomain host", host: "staging.platform.claude.com", want: "platform"},
		{name: "oma platform host", host: "oma.duck.ai", want: "platform"},
		{name: "oma platform host with port", host: "oma.duck.ai:443", want: "platform"},
		{name: "localhost frontend host", host: "localhost:5173", want: "platform"},
		{name: "localhost frontend host with api key", host: "localhost:5173", apiKey: "sk-ant-local-test", want: "service"},
		{name: "local IPv4 frontend host", host: "127.0.0.1:5173", want: "platform"},
		{name: "local IPv4 frontend host with bearer token", host: "127.0.0.1:5173", apiKey: "Bearer sk-ant-local-test", want: "service"},
		{name: "local IPv6 frontend host", host: "[::1]:5173", want: "platform"},
		{name: "api host", host: "api.anthropic.com", want: "service"},
		{name: "local host", host: "127.0.0.1:18080", want: "service"},
		{name: "localhost backend host", host: "localhost:38080", want: "service"},
		{name: "local IPv6 backend host", host: "[::1]:38080", want: "service"},
		{name: "empty host", host: "", want: "service"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/files", nil)
			req.Host = tt.host
			if strings.HasPrefix(tt.apiKey, "Bearer ") {
				req.Header.Set("Authorization", tt.apiKey)
			} else if tt.apiKey != "" {
				req.Header.Set("X-Api-Key", tt.apiKey)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			got := string(mustReadAll(t, rec.Result().Body))
			if got != tt.want {
				t.Fatalf("entrypoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func mustReadAll(t *testing.T, body io.ReadCloser) []byte {
	t.Helper()
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return data
}
