package observability

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

var ErrHistoryGap = errors.New("trajectory history gap")
var ErrEventConflict = errors.New("trajectory event conflict")

func InvalidTimeRange() error {
	return apperr.New(apperr.InvalidArgument, "invalid trajectory time range", nil)
}

func InvalidTimeWindow() error {
	return apperr.New(apperr.InvalidArgument, "trajectory time range must be positive and at most one year", nil)
}

func InvalidCursor() error {
	return apperr.New(apperr.InvalidArgument, "invalid trajectory cursor", nil)
}

func InvalidOffset() error {
	return apperr.New(apperr.InvalidArgument, "invalid trajectory offset", nil)
}

func MissingTrajectoryID() error {
	return apperr.New(apperr.InvalidArgument, "trajectory id is required", nil)
}

func invalidStorageScope() error {
	return apperr.New(apperr.Internal, "invalid trajectory storage scope", nil)
}

func historyGap(expected, actual int64) error {
	return apperr.New(apperr.Unavailable, "trajectory history has a sequence gap", fmt.Errorf("%w: expected %d, got %d", ErrHistoryGap, expected, actual))
}

func eventConflict() error {
	return apperr.New(apperr.Conflict, "trajectory contains conflicting events", ErrEventConflict)
}

func unavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "trajectory storage is unavailable", cause)
}

func invalidResponse(cause error) error {
	return apperr.New(apperr.Internal, "invalid trajectory storage response", cause)
}

func notFound() error {
	return apperr.New(apperr.NotFound, "trajectory resource not found", nil)
}
