package environments

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var (
	errPrebuildUnsupported      = errors.New("this provider does not support this build operation")
	errPrebuildUnavailable      = errors.New("package build provider is not configured for this build")
	errPrebuildSuperseded       = errors.New("Build no longer belongs to the current environment configuration.")
	errPrebuildConflict         = errors.New("package build changed; refresh before retrying this operation")
	errPrebuildStageNotStarted  = errors.New("Build stage has not started yet")
	errPrebuildStage            = errors.New("stage must be image or template")
	errPrebuildCursor           = errors.New("invalid build log cursor")
	errPrebuildLogCursorExpired = errors.New("Build log changed; reload logs from the beginning")
	errPrebuildMissingOutput    = errors.New("successful build is missing its output")
	// errPrebuildMissingBuildRef reports a provider that accepted the submission
	// without returning a reference, so remote work may have started unobserved.
	errPrebuildMissingBuildRef = errors.New("build provider accepted the submission without returning a build reference")
	// errPrebuildSubmissionRejected ends a job whose submission the provider
	// rejected before starting any remote work.
	errPrebuildSubmissionRejected = errors.New("Build provider rejected the environment prebuild submission.")
	// errPrebuildSubmissionUnknown ends a job whose submission cannot be proven
	// either way from the provider response.
	errPrebuildSubmissionUnknown = errors.New("Submission outcome unknown; inspect the provider before retrying.")
)

// prebuildSubmissionRejectedMessage is the operator-facing reason for a
// rejected submission. It carries the provider status because the operator has
// to diagnose the failure without reading server logs.
func prebuildSubmissionRejectedMessage(status int) string {
	return fmt.Sprintf("Build provider rejected the submission with HTTP %d; no remote build was started. Check the provider credentials and endpoint configuration.", status)
}

func prebuildError(err error) error {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return apperr.New(apperr.NotFound, "Environment not found", err)
	case errors.Is(err, errPrebuildStageNotStarted):
		return apperr.New(apperr.Conflict, errPrebuildStageNotStarted.Error(), err)
	case errors.Is(err, errPrebuildLogCursorExpired):
		return apperr.New(apperr.Conflict, errPrebuildLogCursorExpired.Error(), err)
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

func invalidEnvironmentListStatus() error {
	return apperr.New(apperr.InvalidArgument, "status must be all, active or archived", nil)
}
