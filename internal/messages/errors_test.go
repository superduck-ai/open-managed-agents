package messages

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestProviderResolveErrorDistinguishesMissingProviderFromLoadFailure(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
		wantMsg    string
	}{
		{
			name:       "model not configured",
			err:        llmproviders.ErrModelNotConfigured,
			wantStatus: http.StatusBadRequest,
			wantType:   "invalid_request_error",
			wantMsg:    "Model is not configured for this workspace",
		},
		{
			name:       "provider missing",
			err:        llmproviders.ErrNotConfigured,
			wantStatus: http.StatusServiceUnavailable,
			wantType:   "api_error",
			wantMsg:    "This workspace has no LLM provider configured",
		},
		{
			name:       "ambiguous model",
			err:        llmproviders.ErrAmbiguousModel,
			wantStatus: http.StatusInternalServerError,
			wantType:   "api_error",
			wantMsg:    "Workspace model configuration is unavailable",
		},
		{
			name:       "incomplete secret envelope",
			err:        fmt.Errorf("open LLM provider API key: %w", db.ErrIncompleteLLMProviderSecret),
			wantStatus: http.StatusInternalServerError,
			wantType:   "api_error",
			wantMsg:    "Workspace model configuration is unavailable",
		},
		{
			name:       "load failure",
			err:        errors.New("database unavailable"),
			wantStatus: http.StatusInternalServerError,
			wantType:   "api_error",
			wantMsg:    "Workspace model configuration is unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := providerResolveError(test.err)
			if mapped.Status != test.wantStatus || mapped.Type != test.wantType || mapped.Message != test.wantMsg {
				t.Fatalf("error = %+v, want status %d type %q message %q", mapped, test.wantStatus, test.wantType, test.wantMsg)
			}
		})
	}
}
