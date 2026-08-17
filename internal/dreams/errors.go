package dreams

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func dreamsBetaRequired() error {
	return apperr.New(apperr.InvalidArgument, "Dreams API requires anthropic-beta: dreaming-2026-04-21", nil)
}

func dreamRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func dreamAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func dreamNotFound(dreamID string, cause error) error {
	return apperr.New(apperr.NotFound, "Dream not found: "+dreamID, cause)
}

func memoryStoreNotFound(memoryStoreID string, cause error) error {
	return apperr.New(apperr.NotFound, "Memory store not found: "+memoryStoreID, cause)
}

func sessionNotFound(sessionID string, cause error) error {
	return apperr.New(apperr.NotFound, "Session not found: "+sessionID, cause)
}

func dreamCannotTransition(cause error) error {
	return apperr.New(apperr.InvalidState, "dream state does not allow this operation", cause)
}

func mapDreamLoadError(err error, dreamID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return dreamNotFound(dreamID, err)
	}
	return internalError("Dream operation failed", fmt.Errorf("dream %q operation: %w", dreamID, err))
}
