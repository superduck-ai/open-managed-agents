package models

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

func modelRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func modelAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func modelLookupFailed(modelID string, err error) error {
	if errors.Is(err, llmproviders.ErrAmbiguousModel) {
		return apperr.New(apperr.InvalidArgument, err.Error(), err)
	}
	return apperr.New(apperr.NotFound, fmt.Sprintf("model: %s", modelID), err)
}

func modelUnavailable(err error) error {
	if errors.Is(err, llmproviders.ErrNotConfigured) {
		return apperr.New(
			apperr.Unavailable,
			"This workspace has no LLM provider configured",
			err,
		)
	}
	return apperr.New(
		apperr.Internal,
		"Workspace model configuration is unavailable",
		err,
	)
}
