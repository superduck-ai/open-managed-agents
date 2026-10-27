package sessions

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestSessionThreadRoutesRequireManagedBeta(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(config.Config{}, nil, &codesessions.Service{}, nil, nil, nil, logger)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/sesn_contract/threads"},
		{http.MethodGet, "/sesn_contract/threads/sthr_contract"},
		{http.MethodPost, "/sesn_contract/threads/sthr_contract/archive"},
		{http.MethodGet, "/sesn_contract/threads/sthr_contract/events"},
		{http.MethodGet, "/sesn_contract/threads/sthr_contract/stream"},
	}
	for _, route := range routes {
		for _, query := range []string{"", "?beta=true"} {
			t.Run(route.method+route.path+query, func(t *testing.T) {
				response := httptest.NewRecorder()
				h.ServeHTTP(response, httptest.NewRequest(route.method, route.path+query, nil))
				if response.Code != http.StatusNotFound {
					t.Fatalf("status=%d want404 without managed Beta: %s", response.Code, response.Body.String())
				}
				var body struct {
					Error struct {
						Type string `json:"type"`
					} `json:"error"`
				}
				if json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Error.Type != "not_found_error" {
					t.Fatalf("wrong public missing-route error: %s", response.Body.String())
				}
			})
		}
	}
}

func TestThreadBetaMiddlewareExternalAndConsoleContracts(t *testing.T) {
	h := NewHandler(config.Config{}, nil, &codesessions.Service{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := h.requireThreadBeta(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		name, path, credential string
		betas                  []string
		want                   int
	}{
		{name: "missing", path: "/", want: http.StatusNotFound},
		{name: "wrong beta", path: "/", betas: []string{"other-beta"}, want: http.StatusNotFound},
		{name: "empty beta", path: "/", betas: []string{""}, want: http.StatusNotFound},
		{name: "substring is not opt in", path: "/", betas: []string{"managed-agents-2026-04-01-extra"}, want: http.StatusNotFound},
		{name: "query does not opt in API key", path: "/?beta=true", credential: auth.CredentialTypeAPIKey, want: http.StatusNotFound},
		{name: "query does not opt in absent principal", path: "/?beta=true", want: http.StatusNotFound},
		{name: "console needs explicit opt in", path: "/", credential: auth.CredentialTypePlatformSession, want: http.StatusNotFound},
		{name: "console false is not opt in", path: "/?beta=false", credential: auth.CredentialTypePlatformSession, want: http.StatusNotFound},
		{name: "documented header only", path: "/", credential: auth.CredentialTypeAPIKey, betas: []string{"managed-agents-2026-04-01"}, want: http.StatusNoContent},
		{name: "documented header with SDK query", path: "/?beta=true", credential: auth.CredentialTypeAPIKey, betas: []string{"managed-agents-2026-04-01"}, want: http.StatusNoContent},
		{name: "comma separated betas", path: "/", betas: []string{"other-beta, managed-agents-2026-04-01 "}, want: http.StatusNoContent},
		{name: "separate header values", path: "/", betas: []string{"other-beta", "managed-agents-2026-04-01"}, want: http.StatusNoContent},
		{name: "authenticated console query", path: "/?beta=true", credential: auth.CredentialTypePlatformSession, want: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			for _, beta := range test.betas {
				request.Header.Add("anthropic-beta", beta)
			}
			if test.credential != "" {
				request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{CredentialType: test.credential}))
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want%d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestSessionThreadRoutesWithBetaKeepResourceAuthentication(t *testing.T) {
	h := NewHandler(config.Config{}, nil, &codesessions.Service{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, path := range []string{"/sesn_contract/threads", "/sesn_contract/threads/sthr_contract", "/sesn_contract/threads/sthr_contract/events", "/sesn_contract/threads/sthr_contract/stream"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("path=%s status=%d want existing401 authentication", path, response.Code)
		}
	}
}
