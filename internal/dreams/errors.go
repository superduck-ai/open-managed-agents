package dreams

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var errDreamAlreadyTerminal = errors.New("dream contract already closed")

func invalidRequest(err error) error { return apperr.New(apperr.InvalidArgument, err.Error(), err) }
func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}
func contractRequired() error {
	return apperr.New(apperr.InvalidArgument, "Dreams API requires anthropic-version and anthropic-beta: managed-agents-2026-04-01,dreaming-2026-04-21", nil)
}
func authenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing workspace credential", nil)
}
func notFound() error { return apperr.New(apperr.NotFound, "Dream not found", nil) }
func dreamLoadError(err error, id string) error {
	if errors.Is(err, db.ErrNotFound) {
		return notFound()
	}
	return internalError("Dream operation failed", fmt.Errorf("load or mutate dream %q: %w", id, err))
}

func dreamInternalSessionSelected() error {
	return invalidRequest(errors.New("sessions.session_ids must not include a Dream internal Session"))
}

func cancelDreamStatusError(status string) error {
	switch status {
	case "pending", "running", "canceled":
		return nil
	case "completed", "failed":
		return invalidRequest(errors.New("Dream has already ended"))
	default:
		return invalidRequest(fmt.Errorf("Dream status %s cannot be canceled", status))
	}
}

func archiveDreamStatusError(status string) error {
	switch status {
	case "pending", "running":
		return invalidRequest(errors.New("cancel the Dream before archiving"))
	default:
		return nil
	}
}

func reviewSessionError(session db.Session) error {
	if sessionIsDreamInternal(session) {
		return dreamInternalSessionSelected()
	}
	return nil
}
