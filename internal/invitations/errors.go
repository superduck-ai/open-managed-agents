package invitations

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func verifiedEmailRequired() error {
	return apperr.New(apperr.Unauthenticated, "A verified login email is required", nil)
}

func mapDBError(err error) error {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return apperr.New(apperr.NotFound, "Invitation not found", err)
	case errors.Is(err, db.ErrInvitationExpired):
		return apperr.New(apperr.Conflict, "Invitation has expired", err)
	case errors.Is(err, db.ErrInvitationRevoked):
		return apperr.New(apperr.Conflict, "Invitation has been revoked", err)
	case errors.Is(err, db.ErrInvitationConflict):
		return apperr.New(apperr.Conflict, "Invitation can no longer be processed", err)
	default:
		return apperr.New(apperr.Internal, "Could not process invitation", err)
	}
}
