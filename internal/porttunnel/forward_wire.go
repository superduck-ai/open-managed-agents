package porttunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	forwardPathPrefix     = "/internal/session-ports/"
	forwardProtocolHeader = "X-OMA-Port-Protocol"
	forwardFlowHeader     = "X-OMA-Port-Flow-Control"
	maxForwardFailure     = 4 << 10

	forwardNoTunnel          = "no_tunnel"
	forwardTooManyRequests   = "too_many_requests"
	forwardTooManyWebSockets = "too_many_websockets"
	forwardUpstream          = "upstream"
	forwardTimeout           = "timeout"
	forwardDisconnected      = "disconnected"
	forwardUnauthorized      = "unauthorized"
	forwardBadRequest        = "bad_request"
)

type forwardRequest struct {
	Method    string   `json:"method,omitempty"`
	Path      string   `json:"path"`
	Headers   []Header `json:"headers,omitempty"`
	Protocols []string `json:"protocols,omitempty"`
}

type forwardMeta struct {
	Status      int      `json:"status"`
	Headers     []Header `json:"headers,omitempty"`
	FlowControl bool     `json:"flow_control,omitempty"`
}

type forwardFailure struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func forwardHTTPPath(codeSessionUUID string, port int) string {
	return forwardPathPrefix + codeSessionUUID + "/" + strconv.Itoa(port)
}

func forwardWebSocketPath(codeSessionUUID string, port int) string {
	return forwardHTTPPath(codeSessionUUID, port) + "/ws"
}

func encodeForwardRequest(request forwardRequest) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeForwardRequest(value string) (forwardRequest, error) {
	var request forwardRequest
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err == nil {
		err = json.Unmarshal(raw, &request)
	}
	if err == nil && request.Path == "" {
		err = errors.New("missing forwarded path")
	}
	return request, err
}

func forwardFailureFor(err error) (int, forwardFailure) {
	var upstream *UpstreamError
	switch {
	case errors.Is(err, errForwardSignature):
		return http.StatusUnauthorized, forwardFailure{Code: forwardUnauthorized}
	case errors.Is(err, ErrNoTunnel):
		return http.StatusNotFound, forwardFailure{Code: forwardNoTunnel}
	case errors.Is(err, ErrTooManyRequests):
		return http.StatusTooManyRequests, forwardFailure{Code: forwardTooManyRequests}
	case errors.Is(err, ErrTooManyWebSockets):
		return http.StatusTooManyRequests, forwardFailure{Code: forwardTooManyWebSockets}
	case errors.As(err, &upstream):
		return http.StatusBadGateway, forwardFailure{Code: forwardUpstream, Message: upstream.Message}
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, forwardFailure{Code: forwardTimeout}
	default:
		return http.StatusBadGateway, forwardFailure{Code: forwardDisconnected}
	}
}

func (f forwardFailure) err() error {
	switch f.Code {
	case forwardNoTunnel:
		return ErrTunnelGone
	case forwardTooManyRequests:
		return ErrTooManyRequests
	case forwardTooManyWebSockets:
		return ErrTooManyWebSockets
	case forwardUpstream:
		return &UpstreamError{Message: f.Message}
	case forwardTimeout:
		return context.DeadlineExceeded
	default:
		return fmt.Errorf("%w: forward %s", ErrTunnelDisconnected, f.Code)
	}
}

func writeForwardFailure(w http.ResponseWriter, err error) {
	status, failure := forwardFailureFor(err)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(failure)
}

func forwardWriteTimeout(flowControl bool) time.Duration {
	if flowControl {
		return FlowControlledWriteTimeout
	}
	return StallTimeout
}
