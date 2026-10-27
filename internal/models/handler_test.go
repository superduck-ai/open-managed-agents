package models

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func TestModelResponsesContainOnlyConfiguredIDs(t *testing.T) {
	models := modelResponses(llmproviders.NewCatalog([]db.LLMProvider{{Slug: "test", Models: db.LLMModels{{ID: "kimi-k2.5", Name: "Kimi"}, {ID: "qwen3-coder-plus"}}}}))
	if len(models) != 2 {
		t.Fatalf("modelResponses() len = %d", len(models))
	}
	if models[0].Type != "model" || models[0].ID != "test/kimi-k2.5" ||
		models[0].DisplayName != "Kimi" || models[0].CreatedAt != unknownModelCreatedAt ||
		models[0].Capabilities != nil || models[0].MaxInputTokens != nil || models[0].MaxTokens != nil ||
		models[1].Type != "model" || models[1].ID != "test/qwen3-coder-plus" ||
		models[1].DisplayName != "test/qwen3-coder-plus" || models[1].CreatedAt != unknownModelCreatedAt {
		t.Fatalf("models = %#v", models)
	}
}

func TestModelUnavailableDistinguishesMissingProviderFromLoadFailure(t *testing.T) {
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
			name:     "load failure",
			err:      errors.New("database unavailable"),
			wantKind: apperr.Internal,
			wantMsg:  "Workspace model configuration is unavailable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var appErr *apperr.Error
			if !errors.As(modelUnavailable(test.err), &appErr) {
				t.Fatal("expected application error")
			}
			if appErr.Kind != test.wantKind || appErr.PublicMessage != test.wantMsg {
				t.Fatalf("error = %+v", appErr)
			}
		})
	}
}
