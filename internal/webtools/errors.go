package webtools

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func invalidRequest(message string) error {
	return apperr.New(apperr.InvalidArgument, message, nil)
}

func denied(message string) error {
	return apperr.New(apperr.PermissionDenied, message, nil)
}

func unavailable() error {
	return apperr.New(apperr.Unavailable, "Web tools are not configured on the OMA server", nil)
}

func invalidResponse() error {
	return apperr.New(apperr.Unavailable, "Web service returned an invalid or oversized response", nil)
}

func requestFailed() error {
	return apperr.New(apperr.Unavailable, "Web service request failed or timed out", nil)
}

func invalidCredentialFile() error {
	return apperr.New(apperr.InvalidArgument, "Could not load the web tools API key file", nil)
}

func upstreamFailure(status int) error {
	kind, message := apperr.Unavailable, "Web service request failed"
	switch status {
	case http.StatusUnauthorized:
		message = "Web service credential was rejected"
	case http.StatusPaymentRequired:
		message = "Web service balance is insufficient"
	case http.StatusForbidden:
		kind, message = apperr.PermissionDenied, "Web fetch was blocked by the service safety policy"
	case http.StatusNotFound:
		kind, message = apperr.NotFound, "Web page content is unavailable"
	case http.StatusTooManyRequests:
		kind, message = apperr.RateLimited, "Web service rate limit exceeded"
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		kind, message = apperr.Timeout, "Web service timed out"
	}
	return apperr.New(kind, message, nil)
}
