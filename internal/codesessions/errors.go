package codesessions

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

var errRuntimeSubagentConfig = errors.New("invalid runtime subagent configuration")
var errSandboxUnavailable = errors.New("sandbox is unavailable")
var errSessionIngressIdentity = errors.New("session ingress identity does not match an active code session")

var errSandboxCredentialUnbound = errors.New("sandbox credential requires a sandbox UUID")
var errSandboxCredentialScope = errors.New("invalid session ingress credential scope")
var errSandboxCredentialPath = errors.New("sandbox credential does not match request path")
var errSandboxCredentialLifecycle = errors.New("sandbox credential requires persistent lifecycle validation")

var ErrWorkerEventUnavailable = errors.New("worker event transport unavailable")

const maxSandboxMCPRequestBytes = 1 << 20

func invalidWebToolRequest() error {
	return apperr.New(apperr.InvalidArgument, "Invalid web tool request", nil)
}

func webToolDenied() error {
	return apperr.New(apperr.PermissionDenied, "Web tool is disabled by the Agent permission policy", nil)
}

func webToolContextFailure(cause error) error {
	if errors.Is(cause, db.ErrNotFound) {
		return apperr.New(apperr.PermissionDenied, "Could not authorize web tool session or network policy", cause)
	}
	if app, ok := errors.AsType[*apperr.Error](cause); ok && app.Kind == apperr.PermissionDenied {
		return app
	}
	return apperr.New(apperr.Internal, "Could not load web tool session or network policy", cause)
}

func sandboxInvalidRequest(cause error) error {
	return apperr.New(apperr.InvalidArgument, "Invalid sandbox MCP request", cause)
}
func sandboxRequestReadError(cause error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(cause, &tooLarge) {
		return apperr.New(apperr.InvalidArgument, "Sandbox MCP request exceeds 1 MiB; split large file writes into smaller edits", cause)
	}
	return sandboxInvalidRequest(cause)
}
func sandboxToolDenied() error {
	return apperr.New(apperr.PermissionDenied, "Sandbox tool is not permitted for this session", nil)
}
func sandboxOperationError(cause error) error {
	if errors.Is(cause, errSandboxUnavailable) {
		return apperr.New(apperr.Unavailable, "Sandbox is unavailable", cause)
	}
	if errors.Is(cause, db.ErrNotFound) {
		return codeSessionNotFound(cause)
	}
	if errors.Is(cause, db.ErrInvalidState) || errors.Is(cause, db.ErrWorkerEpochMismatch) {
		return apperr.New(apperr.Conflict, "Worker epoch or sandbox state conflict", cause)
	}
	return internalError("Sandbox operation failed", cause)
}

var (
	ErrMCPDeclarationInvalid          = errors.New("MCP server declarations must have unique canonical names and valid targets")
	ErrMCPGatewayMissing              = errors.New("code_session.sandbox_api_base_url is required for managed-agent MCP tunnels")
	ErrMCPRuntimeIdentityMissing      = errors.New("managed-agent MCP Tunnel runtime identity is incomplete")
	errInboundPayloadTooLarge         = errors.New("worker event payload exceeds 16 MiB")
	errLargePayloadStorageUnavailable = errors.New("worker event payload object storage is unavailable")
	errLargePayloadDigestMismatch     = errors.New("worker event payload digest mismatch")
	errActivationSnapshotChanged      = errors.New("code session activation snapshot changed")
)

func codeSessionNotFound(cause error) error {
	return apperr.New(apperr.NotFound, "Code session not found", cause)
}

func codeSessionNotActive(cause error) error {
	return apperr.New(apperr.Conflict, "Code session is not active", cause)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func sessionModelBackendsFailure(cause error) error {
	return internalError("Could not resolve model API backends", cause)
}

func sessionMCPConfigFailure(cause error) error {
	return internalError("Could not build session MCP configuration", cause)
}

func mcpUpstreamAuthenticationRejected() *httpapi.Error {
	return httpapi.NewError(http.StatusUnauthorized, "authentication_error", "MCP upstream authentication failed")
}

func codeSessionRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func sessionIngressTokenRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing session ingress token", nil)
}

func sessionIngressTokenInvalid(cause error) error {
	return apperr.New(apperr.Unauthenticated, "Invalid session ingress token", cause)
}

func invalidSignCommitRequest(message string, cause error) error {
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func signCommitRequestTooLarge(cause error) error {
	return apperr.New(apperr.RequestTooLarge, "Request body exceeds maximum size", cause)
}

func signCommitFailure(cause error) error {
	return internalError("Could not sign commit", cause)
}

func workerEventStreamUnavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "Could not connect code session worker stream", cause)
}

// workerEventUnavailable 将传输层失败包装为 503 应用错误，同时保留
// ErrWorkerEventUnavailable sentinel，供调用方与测试用 errors.Is 识别。
func workerEventUnavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "Could not deliver events to the code session worker", fmt.Errorf("%w: %w", ErrWorkerEventUnavailable, cause))
}

func mapCodeSessionLoadError(err error, codeSessionID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return codeSessionNotFound(err)
	}
	return internalError(
		"Could not load code session",
		fmt.Errorf("load code session %q: %w", codeSessionID, err),
	)
}

func invalidWorkerPayload(message string, cause error) error {
	return &workerPayloadError{message: message, cause: cause}
}

type workerPayloadError struct {
	message string
	cause   error
}

func (e *workerPayloadError) Error() string { return e.message }

func (e *workerPayloadError) Unwrap() error { return e.cause }

func selfHostedResultRoutingError(stage string, err error) error {
	return fmt.Errorf("self-hosted tool result %s: %w", stage, err)
}

func brainClaimUnauthenticated() error {
	return apperr.New(apperr.Unauthenticated, "Invalid brain claim token", nil)
}

func brainClaimInvalidRequest(message string, cause error) error {
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func brainClaimFailure(cause error) error {
	return internalError("Could not claim code session", cause)
}

func mapBrainClaimReleaseError(err error) error {
	if errors.Is(err, db.ErrPreconditionFailed) {
		return apperr.New(apperr.Conflict, "Brain claim is no longer held", err)
	}
	return internalError("Could not release code session claim", err)
}

func workerIngressAuthorizationFailure(cause error) error {
	if errors.Is(cause, errSessionIngressIdentity) {
		return apperr.New(apperr.Unauthenticated, "Session ingress identity is no longer active", cause)
	}
	if errors.Is(cause, db.ErrWorkerEpochMismatch) {
		return apperr.New(apperr.Conflict, "Worker epoch mismatch", cause)
	}
	return internalError("Could not authorize code session worker", cause)
}

var ErrModelPriceUnavailable = errors.New("model price unavailable for budgeted session")
