package telemetrydestinations

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const BetaHeader = "telemetry-destinations-2026-08-11"

func betaRequired() error {
	return apperr.New(apperr.InvalidArgument, "telemetry destinations require anthropic-beta: "+BetaHeader, nil)
}

func invalidRequest(message string) error {
	return apperr.New(apperr.InvalidArgument, message, nil)
}

func invalidRequestf(format string, args ...any) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf(format, args...), nil)
}

func invalidBody(cause error) error {
	return apperr.New(apperr.InvalidArgument, "request body must be a JSON object matching the telemetry destination schema", cause)
}

func notFound(id string) error {
	return apperr.New(apperr.NotFound, fmt.Sprintf("telemetry destination %s not found", id), nil)
}

func permissionDenied() error {
	return apperr.New(apperr.PermissionDenied, "organization administrator required to modify telemetry destinations", nil)
}

func organizationNotFound() error {
	return apperr.New(apperr.NotFound, "organization not found", nil)
}

func destinationLimit(limit int) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("an organization can have at most %d telemetry destinations", limit), nil)
}

func concurrentModification() error {
	return apperr.New(apperr.Conflict, "telemetry destination was modified concurrently; retry the request", nil)
}

func testTooSoon() error {
	return apperr.New(apperr.RateLimited, "telemetry destination tests must be at least 10 seconds apart", nil)
}

func internalError(cause error) error {
	return apperr.New(apperr.Internal, "telemetry destination request failed", cause)
}

func mapStoreError(id string, err error) error {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return notFound(id)
	case errors.Is(err, db.ErrVersionConflict):
		return concurrentModification()
	default:
		return internalError(err)
	}
}
