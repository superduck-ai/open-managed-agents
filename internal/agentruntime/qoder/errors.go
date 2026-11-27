package qoder

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

var (
	ErrUpstreamAuth        = errors.New("qoder: upstream authentication failed")
	ErrUpstreamBilling     = errors.New("qoder: upstream credits exhausted or payment required")
	ErrUpstreamNotFound    = errors.New("qoder: upstream resource not found")
	ErrUpstreamConflict    = errors.New("qoder: upstream resource conflict")
	ErrUpstreamRateLimited = errors.New("qoder: upstream rate limited")
	ErrUpstreamUnavailable = errors.New("qoder: upstream service unavailable")
	ErrDownloadBlocked     = errors.New("qoder: download target blocked by SSRF policy")
	ErrInvalidCredentials  = errors.New("qoder: invalid runtime credentials")
	ErrInvalidEndpoint     = errors.New("qoder: invalid runtime endpoint")
)

type upstreamAPIError struct {
	StatusCode           int
	ErrorType            string
	Message              string
	ReferencedSessionIDs []string
}

func (e *upstreamAPIError) Error() string {
	if e == nil {
		return ""
	}
	if e.ErrorType != "" && e.Message != "" {
		return fmt.Sprintf("qoder upstream status %d (%s): %s", e.StatusCode, e.ErrorType, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("qoder upstream status %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("qoder upstream status %d", e.StatusCode)
}

type upstreamErrorEnvelope struct {
	Type                 string               `json:"type"`
	Message              string               `json:"message"`
	ReferencedSessionIDs []string             `json:"referenced_session_ids"`
	Error                *upstreamErrorDetail `json:"error"`
}

type upstreamErrorDetail struct {
	Type                 string   `json:"type"`
	Message              string   `json:"message"`
	ReferencedSessionIDs []string `json:"referenced_session_ids"`
}

func parseUpstreamError(statusCode int, body []byte) *upstreamAPIError {
	apiErr := &upstreamAPIError{StatusCode: statusCode}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return apiErr
	}
	var env upstreamErrorEnvelope
	if err := json.Unmarshal(trimmed, &env); err == nil {
		if env.Error != nil {
			apiErr.ErrorType = strings.TrimSpace(env.Error.Type)
			apiErr.Message = strings.TrimSpace(env.Error.Message)
			apiErr.ReferencedSessionIDs = append(apiErr.ReferencedSessionIDs, env.Error.ReferencedSessionIDs...)
		}
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(env.Message)
		}
		if len(apiErr.ReferencedSessionIDs) == 0 && len(env.ReferencedSessionIDs) > 0 {
			apiErr.ReferencedSessionIDs = append(apiErr.ReferencedSessionIDs, env.ReferencedSessionIDs...)
		}
		if apiErr.ErrorType == "" && env.Type != "" && env.Type != "error" {
			apiErr.ErrorType = strings.TrimSpace(env.Type)
		}
		return apiErr
	}
	text := string(trimmed)
	if len(text) > 256 {
		text = text[:256]
	}
	apiErr.Message = text
	return apiErr
}

func readAndMapUpstreamError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return mapUpstreamAPIError(parseUpstreamError(resp.StatusCode, body))
}

func mapUpstreamAPIError(apiErr *upstreamAPIError) error {
	if apiErr == nil {
		return apperr.New(apperr.Unavailable, "qoder upstream request failed", ErrUpstreamUnavailable)
	}
	switch apiErr.StatusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		msg := apiErr.Message
		if msg == "" {
			msg = "invalid request rejected by qoder runtime"
		}
		return apperr.New(apperr.InvalidArgument, msg, apiErr)
	case http.StatusUnauthorized:
		return apperr.New(apperr.Unauthenticated, "qoder runtime authentication failed", errors.Join(ErrUpstreamAuth, apiErr))
	case http.StatusPaymentRequired:
		return apperr.New(apperr.Billing, "qoder runtime credits exhausted or balance insufficient", errors.Join(ErrUpstreamBilling, apiErr))
	case http.StatusForbidden:
		return apperr.New(apperr.PermissionDenied, "qoder runtime access denied", apiErr)
	case http.StatusNotFound:
		msg := apiErr.Message
		if msg == "" {
			msg = "resource not found"
		}
		return apperr.New(apperr.NotFound, msg, errors.Join(agentruntime.ErrResourceNotFound, ErrUpstreamNotFound, apiErr))
	case http.StatusConflict:
		msg := apiErr.Message
		if msg == "" {
			msg = "qoder resource conflict"
		}
		return apperr.New(apperr.Conflict, msg, errors.Join(ErrUpstreamConflict, apiErr))
	case http.StatusRequestEntityTooLarge:
		return apperr.New(apperr.RequestTooLarge, "file exceeds qoder runtime size limit", apiErr)
	case http.StatusTooManyRequests:
		return apperr.New(apperr.RateLimited, "qoder runtime rate limit exceeded", errors.Join(ErrUpstreamRateLimited, apiErr))
	case http.StatusNotImplemented:
		msg := apiErr.Message
		if msg == "" {
			msg = "feature is not available on qoder runtime"
		}
		return apperr.New(apperr.NotImplemented, msg, apiErr)
	case http.StatusGatewayTimeout:
		return apperr.New(apperr.Timeout, "qoder runtime request timed out", errors.Join(ErrUpstreamUnavailable, apiErr))
	case 529:
		return apperr.New(apperr.Overloaded, "qoder runtime is overloaded", errors.Join(ErrUpstreamUnavailable, apiErr))
	default:
		if apiErr.StatusCode >= 500 {
			return apperr.New(apperr.Unavailable, "qoder runtime temporarily unavailable", errors.Join(ErrUpstreamUnavailable, apiErr))
		}
		return apperr.New(apperr.Unavailable, "qoder runtime returned unexpected error", errors.Join(ErrUpstreamUnavailable, apiErr))
	}
}

func invalidCredentialsError(message string, cause error) error {
	if cause == nil {
		cause = ErrInvalidCredentials
	} else {
		cause = errors.Join(ErrInvalidCredentials, cause)
	}
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func invalidEndpointError(message string, cause error) error {
	if cause == nil {
		cause = ErrInvalidEndpoint
	} else {
		cause = errors.Join(ErrInvalidEndpoint, cause)
	}
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func downloadBlockedError(reason string) error {
	return apperr.New(apperr.PermissionDenied, "file download blocked by security policy", fmt.Errorf("%w: %s", ErrDownloadBlocked, reason))
}

func fileTooLargeError(maxBytes int64) error {
	return apperr.New(apperr.RequestTooLarge, fmt.Sprintf("file size exceeds maximum limit of %d bytes", maxBytes), nil)
}

func unsupportedFileTypeError(mimeType string) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("file mime type %q is not supported by qoder runtime", mimeType), nil)
}

func upstreamTransportError(cause error) error {
	return apperr.New(apperr.Unavailable, "failed to reach qoder runtime", errors.Join(ErrUpstreamUnavailable, cause))
}

func unhealthyError(message string, cause error) error {
	return apperr.New(apperr.Unavailable, message, errors.Join(ErrUpstreamUnavailable, cause))
}
