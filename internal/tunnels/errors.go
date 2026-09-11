package tunnels

import (
	"context"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var (
	ErrControlNotFound      = errors.New("tunnels: control state not found")
	ErrCleanupUnavailable   = errors.New("tunnels: cleanup jobs are not configured")
	ErrNoConnector          = errors.New("tunnels: no live connector")
	ErrQueueLimit           = errors.New("tunnels: pending request limit exceeded")
	ErrPayloadLimit         = errors.New("tunnels: payload limit exceeded")
	ErrChannelLimit         = errors.New("tunnels: channel limit exceeded")
	ErrChannelInvalid       = errors.New("tunnels: channel is invalid")
	ErrChannelMismatch      = errors.New("tunnels: channel declaration mismatch")
	ErrRequestNotFound      = errors.New("tunnels: request not found")
	ErrResponseMismatch     = errors.New("tunnels: response binding mismatch")
	ErrRequestExpired       = errors.New("tunnels: request expired")
	ErrRequestCanceled      = errors.New("tunnels: request canceled")
	ErrTokenRetired         = errors.New("tunnels: token version is no longer active")
	ErrBrokerBusy           = errors.New("tunnels: broker is busy")
	ErrResponseBackpressure = errors.New("tunnels: response buffer is full")
	ErrSessionNotFound      = errors.New("tunnels: MCP session is no longer available; initialize a new session")
)

func connectorResponseError(err error) error {
	switch {
	case errors.Is(err, ErrRequestNotFound), errors.Is(err, ErrResponseMismatch), errors.Is(err, ErrRequestExpired), errors.Is(err, ErrRequestCanceled):
		return connectorRequestNotFound()
	case errors.Is(err, ErrResponseBackpressure):
		return apperr.New(apperr.RateLimited, "Tunnel response buffer is full", err)
	default:
		return unavailable("Tunnel broker is unavailable", err)
	}
}

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func betaRequired() error {
	return apperr.New(
		apperr.InvalidArgument,
		"Tunnels API requires anthropic-beta: "+currentBeta,
		nil,
	)
}

func missingAPIKey() error {
	return apperr.New(apperr.Unauthenticated, "API key authentication required", nil)
}

func invalidConnectorCredential() error {
	return apperr.New(apperr.Unauthenticated, "Invalid tunnel token", nil)
}

func connectorRequestNotFound() error {
	return apperr.New(apperr.NotFound, "Tunnel request not found", nil)
}

func unavailable(message string, cause error) error {
	return apperr.New(apperr.Unavailable, message, cause)
}

func ingressQueueError(err error) error {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		return apperr.New(apperr.NotFound, "MCP session is no longer available; initialize a new session", err)
	case errors.Is(err, ErrNoConnector):
		return unavailable("No tunnel connector is available", err)
	case errors.Is(err, ErrQueueLimit), errors.Is(err, ErrPayloadLimit):
		return apperr.New(apperr.RateLimited, "Tunnel pending request limit exceeded", err)
	default:
		return unavailable("Tunnel broker is unavailable", err)
	}
}

func ingressResponseError(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrRequestExpired):
		return apperr.New(apperr.Timeout, "Tunnel request timed out", err)
	case errors.Is(err, context.Canceled), errors.Is(err, ErrRequestCanceled):
		return apperr.New(apperr.Unavailable, "Tunnel request was canceled", err)
	default:
		return unavailable("Tunnel broker is unavailable", err)
	}
}

func apperrRequestTooLarge(message string, cause error) error {
	return apperr.New(apperr.RequestTooLarge, message, cause)
}

func routeNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func tunnelNotFound(tunnelID string, cause error) error {
	return apperr.New(apperr.NotFound, "Tunnel not found: "+tunnelID, cause)
}

func certificateNotFound(certificateID string, cause error) error {
	return apperr.New(apperr.NotFound, "Tunnel certificate not found: "+certificateID, cause)
}

func archivedTunnel(tunnelID string) error {
	return apperr.New(apperr.InvalidArgument, "Tunnel is archived: "+tunnelID, nil)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func tokenTransitionError(message string, err error) error {
	if errors.Is(err, ErrTokenRetired) {
		return apperr.New(apperr.Conflict, "Tunnel changed concurrently; reload and try again", err)
	}
	return internalError(message, err)
}

func mapTunnelLookupError(err error, tunnelID, operation string) error {
	var appError *apperr.Error
	if errors.As(err, &appError) {
		return appError
	}
	switch {
	case errors.Is(err, db.ErrNotFound):
		return tunnelNotFound(tunnelID, err)
	case errors.Is(err, db.ErrInvalidState):
		return apperr.New(
			apperr.Conflict,
			"Tunnel changed concurrently; reload and try again",
			fmt.Errorf("%s tunnel %q: %w", operation, tunnelID, err),
		)
	}
	return internalError("Could not "+operation+" tunnel", fmt.Errorf("%s tunnel %q: %w", operation, tunnelID, err))
}

func mapCertificateLookupError(err error, certificateID, operation string) error {
	if errors.Is(err, db.ErrNotFound) {
		return certificateNotFound(certificateID, err)
	}
	return internalError(
		"Could not "+operation+" tunnel certificate",
		fmt.Errorf("%s tunnel certificate %q: %w", operation, certificateID, err),
	)
}

func connectorTokenRecoveryError(err error) error {
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrInvalidState) || errors.Is(err, ErrTokenRetired) {
		return invalidConnectorCredential()
	}
	return unavailable("Could not restore tunnel token state", err)
}

func probePaginationError() error {
	return unavailable("Tunnel MCP tool list exceeds pagination limits", errors.New("tool count, page count, or cursor cycle limit exceeded"))
}

func probeToolsResponseError(err error) error {
	return unavailable("Tunnel MCP tools/list failed", err)
}
