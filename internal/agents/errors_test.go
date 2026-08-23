package agents

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestConfiguredModelErrorDistinguishesMissingProviderFromLoadFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantKind apperr.Kind
		wantMsg  string
	}{
		{
			name:     "missing provider",
			err:      llmproviders.ErrNotConfigured,
			wantKind: apperr.Unavailable,
			wantMsg:  "This workspace has no LLM provider configured",
		},
		{
			name:     "ambiguous model",
			err:      llmproviders.ErrAmbiguousModel,
			wantKind: apperr.Internal,
			wantMsg:  "Workspace model configuration is unavailable",
		},
		{
			name:     "load failure",
			err:      errors.New("database unavailable"),
			wantKind: apperr.Internal,
			wantMsg:  "Workspace model configuration is unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var appErr *apperr.Error
			if !errors.As(configuredModelError(test.err), &appErr) {
				t.Fatal("expected application error")
			}
			if appErr.Kind != test.wantKind || appErr.PublicMessage != test.wantMsg {
				t.Fatalf("error = %+v", appErr)
			}
		})
	}
}

func TestAgentMutationErrorPreservesConfiguredModelStatus(t *testing.T) {
	err := agentMutationError(configuredModelError(llmproviders.ErrNotConfigured))
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.Unavailable {
		t.Fatalf("error = %#v, want Unavailable", err)
	}
}
