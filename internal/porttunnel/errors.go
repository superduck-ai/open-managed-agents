package porttunnel

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

var (
	ErrNoTunnel             = errors.New("session port tunnel is not connected")
	ErrTunnelGone           = errors.New("session port tunnel owner is gone")
	ErrTunnelDisconnected   = errors.New("session port tunnel disconnected")
	ErrTooManyRequests      = errors.New("too many concurrent session port requests")
	ErrTooManyWebSockets    = errors.New("too many session port websockets")
	ErrStalled              = errors.New("session port caller stopped reading")
	ErrFlowControlViolation = errors.New("session port tunnel client exceeded the flow control window")
	ErrUnauthorized         = errors.New("invalid session port tunnel credential")
	errReplaced             = errors.New("session port tunnel replaced by a newer connection")
	errShutdown             = errors.New("session port server shutting down")
	errForwardSignature     = errors.New("invalid session port forward signature")
	errProtocol             = errors.New("unexpected session port tunnel frame")
)

type UpstreamError struct {
	Message string
}

func (e *UpstreamError) Error() string {
	return "session port upstream: " + e.Message
}

func IsHeaderTimeout(err error) bool {
	var upstream *UpstreamError
	return errors.As(err, &upstream) && strings.Contains(upstream.Message, "response header timeout")
}

type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	return "session port websocket closed: " + e.Reason
}

func tunnelCredentialMissing() *httpapi.Error {
	return httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing session port tunnel credential")
}

func tunnelCredentialInvalid() *httpapi.Error {
	return httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid session port tunnel credential")
}

func tunnelAuthenticationUnavailable() *httpapi.Error {
	return httpapi.NewError(http.StatusServiceUnavailable, "api_error", "Could not authenticate session port tunnel")
}
