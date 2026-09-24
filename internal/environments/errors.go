package environments

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

var (
	errPrebuildUnsupported   = errors.New("this provider does not support this build operation")
	errPrebuildUnavailable   = errors.New("package build provider is not configured for this build")
	errPrebuildSuperseded    = errors.New("Build no longer belongs to the current environment configuration.")
	errPrebuildConflict      = errors.New("package build changed; refresh before retrying this operation")
	errPrebuildStage         = errors.New("stage must be image or template")
	errPrebuildCursor        = errors.New("invalid build log cursor")
	errPrebuildMissingOutput = errors.New("successful build is missing its output")
)

func prebuildError(err error) error {
	switch {
	case errors.Is(err, errPrebuildConflict):
		return apperr.New(apperr.Conflict, errPrebuildConflict.Error(), err)
	case errors.Is(err, errPrebuildUnsupported), errors.Is(err, errPrebuildUnavailable):
		return apperr.New(apperr.PreconditionFailed, err.Error(), err)
	case errors.Is(err, errPrebuildCursor):
		return invalidRequest(err)
	default:
		return internalError("Could not access package build", err)
	}
}

var errGitResourcesRequireMITM = errors.New("git resources require code_session.upstream_proxy_mitm_enabled")

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
