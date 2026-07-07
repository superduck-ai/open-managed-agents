package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIEntrypointRouterDispatchesByAuth(t *testing.T) {
	handler := apiEntrypointRouter{
		service: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("service"))
		}),
		platform: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("platform"))
		}),
	}
	tests := []struct {
		name       string
		host       string
		apiKey     string
		sessionKey string
		want       string
	}{
		// ---- API key → service (always) ----
		{name: "api key on any host", host: "localhost:5173", apiKey: "sk-ant-local-test", want: "service"},
		{name: "bearer token on any host", host: "localhost:5173", apiKey: "Bearer sk-ant-local-test", want: "service"},
		{name: "api key on api host", host: "api.anthropic.com", apiKey: "sk-ant-test", want: "service"},

		// ---- Session cookie → platform (always) ----
		{name: "session on localhost", host: "localhost:5173", sessionKey: "sk-ant-sid-test", want: "platform"},
		{name: "session on server port", host: "localhost:38080", sessionKey: "sk-ant-sid-test", want: "platform"},
		{name: "session on oma domain", host: "oma.duck.ai", sessionKey: "sk-ant-sid-test", want: "platform"},
		{name: "session on api host", host: "api.anthropic.com", sessionKey: "sk-ant-sid-test", want: "platform"},

		// ---- API key + session cookie → API key wins ----
		{name: "api key wins over session", host: "localhost:5173", apiKey: "sk-ant-local-test", sessionKey: "sk-ant-sid-test", want: "service"},

		// ---- No credential → service (default) ----
		{name: "no auth localhost", host: "localhost:5173", want: "service"},
		{name: "no auth oma", host: "oma.duck.ai", want: "service"},
		{name: "no auth api host", host: "api.anthropic.com", want: "service"},
		{name: "no auth empty host", host: "", want: "service"},
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
			if tt.sessionKey != "" {
				req.AddCookie(&http.Cookie{Name: "sessionKey", Value: tt.sessionKey})
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
