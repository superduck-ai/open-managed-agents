package portgateway

import (
	"net/http"
	"strconv"
	"time"
)

const (
	errorHeader         = "X-OMA-Port-Error"
	wakeRetryAfter      = 5 * time.Second
	codeNotFound        = "not_found"
	codeUnavailable     = "sandbox_unavailable"
	codeTooLarge        = "request_too_large"
	codeTooMany         = "too_many_requests"
	codeDisconnected    = "tunnel_disconnected"
	codeWaking          = "waking"
	codeUpstreamTimeout = "upstream_timeout"
	codeInternal        = "internal_error"
)

type gatewayError struct {
	status     int
	code       string
	message    string
	retryAfter time.Duration
	cause      error
}

func (e *gatewayError) Error() string {
	if e.cause != nil {
		return e.code + ": " + e.cause.Error()
	}
	return e.code
}

func (e *gatewayError) Unwrap() error {
	return e.cause
}

func notFound(cause error) *gatewayError {
	return &gatewayError{status: http.StatusNotFound, code: codeNotFound, message: "Session port not found", cause: cause}
}

func sandboxUnavailable(cause error) *gatewayError {
	return &gatewayError{status: http.StatusConflict, code: codeUnavailable, message: "Session sandbox is not available; send the session a new message to restart it", cause: cause}
}

func requestTooLarge() *gatewayError {
	return &gatewayError{status: http.StatusRequestEntityTooLarge, code: codeTooLarge, message: "Request body is too large"}
}

func tooManyRequests(cause error) *gatewayError {
	return &gatewayError{status: http.StatusTooManyRequests, code: codeTooMany, message: "Too many concurrent requests to this session port", cause: cause}
}

func tunnelDisconnected(cause error) *gatewayError {
	return &gatewayError{status: http.StatusBadGateway, code: codeDisconnected, message: "Session port is unreachable", cause: cause}
}

func waking(cause error) *gatewayError {
	return &gatewayError{status: http.StatusServiceUnavailable, code: codeWaking, message: "Session sandbox is waking up; retry shortly", retryAfter: wakeRetryAfter, cause: cause}
}

func upstreamTimeout(cause error) *gatewayError {
	return &gatewayError{status: http.StatusGatewayTimeout, code: codeUpstreamTimeout, message: "Session port did not respond in time", cause: cause}
}

func internalFailure(cause error) *gatewayError {
	return &gatewayError{status: http.StatusInternalServerError, code: codeInternal, message: "Session port gateway failed", cause: cause}
}

func writeGatewayError(w http.ResponseWriter, failure *gatewayError) {
	header := w.Header()
	header.Set(errorHeader, failure.code)
	header.Set("Content-Type", "text/plain; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set(referrerPolicyHeader, referrerPolicy)
	if failure.retryAfter > 0 {
		header.Set("Retry-After", strconv.Itoa(int(failure.retryAfter/time.Second)))
	}
	w.WriteHeader(failure.status)
	_, _ = w.Write([]byte(failure.message + "\n"))
}
