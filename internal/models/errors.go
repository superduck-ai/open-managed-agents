package models

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func modelRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func modelAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func modelUnavailable(err error) error {
	if errors.Is(err, llmproviders.ErrNotConfigured) {
		return apperr.NewCoded(
			apperr.Unavailable,
			"workspace_llm_provider_not_configured",
			"This workspace has no LLM provider configured",
			err,
		)
	}
	return apperr.NewCoded(
		apperr.Unavailable,
		"workspace_model_configuration_unavailable",
		"Workspace model configuration is unavailable",
		err,
	)
}
