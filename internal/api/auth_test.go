package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestV1FallbacksRequireAuthentication(t *testing.T) {
	t.Parallel()

	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create code session credentials: %v", err)
	}
	server := NewServerWithPlatformSessionsAndCredentials(config.Config{}, nil, nil, nil, nil, credentials)
	for _, test := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "unknown path", method: http.MethodGet, path: "/v1/not-existing"},
		{name: "unsupported method", method: http.MethodDelete, path: "/v1/messages"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if body := response.Body.String(); !strings.Contains(body, `"type":"authentication_error"`) {
				t.Fatalf("body = %q, want authentication_error", body)
			}
		})
	}
}

func TestV1AuthenticationSelection(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		apiKey      string
		sessionKey  string
		wantService bool
	}{
		// ---- API key → service (always) ----
		{name: "api key on any host", host: "localhost:5173", apiKey: "sk-ant-local-test", wantService: true},
		{name: "bearer token on any host", host: "localhost:5173", apiKey: "Bearer sk-ant-local-test", wantService: true},
		{name: "api key on api host", host: "api.anthropic.com", apiKey: "sk-ant-test", wantService: true},

		// ---- Session cookie → platform (always) ----
		{name: "session on localhost", host: "localhost:5173", sessionKey: "sk-ant-sid-test"},
		{name: "session on server port", host: "localhost:38080", sessionKey: "sk-ant-sid-test"},
		{name: "session on oma domain", host: "oma.duck.ai", sessionKey: "sk-ant-sid-test"},
		{name: "session on api host", host: "api.anthropic.com", sessionKey: "sk-ant-sid-test"},

		// ---- API key + session cookie → API key wins ----
		{name: "api key wins over session", host: "localhost:5173", apiKey: "sk-ant-local-test", sessionKey: "sk-ant-sid-test", wantService: true},

		// ---- No credential → platform (default, has unauthenticated routes) ----
		{name: "no auth localhost", host: "localhost:5173"},
		{name: "no auth oma", host: "oma.duck.ai"},
		{name: "no auth api host", host: "api.anthropic.com"},
		{name: "no auth empty host", host: ""},
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
			if got := usesServiceAuthentication(req); got != tt.wantService {
				t.Fatalf("uses service authentication = %t, want %t", got, tt.wantService)
			}
		})
	}
}
