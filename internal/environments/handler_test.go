package environments

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestEnvironmentCreateValidatesBodyRegardlessOfBeta(t *testing.T) {
	handler := NewHandler(config.Config{}, nil, nil)
	tests := []struct {
		name   string
		query  string
		header string
	}{
		{name: "omitted"},
		{name: "false", query: "?beta=false"},
		{name: "legacy", query: "?beta=true", header: "managed-agents-2026-04-01"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/"+tt.query, strings.NewReader(`{}`))
			if tt.header != "" {
				request.Header.Set("Anthropic-Beta", tt.header)
			}
			request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{CredentialType: auth.CredentialTypeAPIKey}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"message":"name is required"`) {
				t.Fatalf("create response = %d %s, want 400 with name validation error", response.Code, response.Body.String())
			}
		})
	}
}

func TestDecodeEnvironmentWorkStopForceRejectsInvalidBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{`},
		{name: "non object", body: `[]`},
		{name: "oversized", body: `{"force":"` + strings.Repeat("x", maxEnvironmentBodySize) + `"}`},
		{name: "invalid force", body: `{"force":"yes"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))

			if _, err := decodeEnvironmentWorkStopForce(httptest.NewRecorder(), request); err == nil {
				t.Fatal("decodeEnvironmentWorkStopForce() error = nil")
			}
		})
	}
}

func TestDecodeEnvironmentWorkStopForcePreservesEmptyBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)

	force, err := decodeEnvironmentWorkStopForce(httptest.NewRecorder(), request)
	if err != nil {
		t.Fatalf("decodeEnvironmentWorkStopForce() error = %v", err)
	}
	if force {
		t.Fatal("decodeEnvironmentWorkStopForce() = true, want false")
	}
}

func TestDecodeEnvironmentWorkStopForceDecodesFirstObject(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"force":true} trailing`))

	force, err := decodeEnvironmentWorkStopForce(httptest.NewRecorder(), request)
	if err != nil {
		t.Fatalf("decodeEnvironmentWorkStopForce() error = %v", err)
	}
	if !force {
		t.Fatal("decodeEnvironmentWorkStopForce() = false, want true")
	}
}
