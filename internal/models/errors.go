package models

import "github.com/superduck-ai/open-managed-agents/internal/apperr"

func modelRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}
