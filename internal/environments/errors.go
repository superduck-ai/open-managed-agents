package environments

import "github.com/superduck-ai/open-managed-agents/internal/apperr"

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func environmentsBetaRequired() error {
	return apperr.New(apperr.InvalidArgument, "Environments API requires beta=true", nil)
}

func environmentRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func environmentAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func environmentNotFound(environmentID string, cause error) error {
	return apperr.New(apperr.NotFound, "Environment not found: "+environmentID, cause)
}

func environmentNameConflict(cause error) error {
	return apperr.New(apperr.Conflict, "Environment name already exists", cause)
}

func environmentHasActiveWork(cause error) error {
	return apperr.New(apperr.InvalidArgument, "Environment has active work", cause)
}

func environmentWorkNotFound(workID string, cause error) error {
	return apperr.New(apperr.NotFound, "Work not found: "+workID, cause)
}

func environmentHeartbeatPreconditionFailed(cause error) error {
	return apperr.New(apperr.PreconditionFailed, "Heartbeat precondition failed", cause)
}
